<script setup lang="ts">
import RepoActionView from './gitea/components/RepoActionView.vue';
import ActionStatusIcon from './gitea/components/ActionStatusIcon.vue';
import {SvgIcon} from './gitea/svg.ts';
import {computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue';
import {buildWorkflowTabs, encodeWorkflowId, normalizeStatus, type RunListItem} from './workflow-tabs.ts';
import 'fomantic-ui-css/semantic.css';
import './gitea_css/themes/theme-gitea-light.css';
import './gitea_css/index.css';
import './tailwind.css';

type ConfirmTone = 'default' | 'danger';
type ToastTone = 'info' | 'success' | 'error';
type ConfirmActionOptions = {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: ConfirmTone;
};
type ConfirmDialogState = Required<ConfirmActionOptions>;
type ToastMessage = {
  id: number;
  text: string;
  tone: ToastTone;
};

const locale = {
  status: {
    unknown: 'Unknown',
    waiting: 'Waiting',
    running: 'Running',
    success: 'Success',
    failure: 'Failure',
    cancelled: 'Cancelled',
    skipped: 'Skipped',
    blocked: 'Blocked',
    cancelling: 'Cancelling',
  },
  summary: 'Summary',
  allJobs: 'Jobs',
  runDetails: 'Run details',
  workflowFile: 'Workflow file',
  workflowDependencies: 'Workflow dependencies',
  graphJobsCount1: '%d job',
  graphJobsCountN: '%d jobs',
  graphDependenciesCount1: '%d dependency',
  graphDependenciesCountN: '%d dependencies',
  graphSuccessRate: 'Success rate %s',
  graphZoomIn: 'Zoom in',
  graphZoomOut: 'Zoom out',
  graphResetView: 'Reset view',
  graphZoomMax: 'Max zoom',
  totalDuration: 'Total duration:',
  triggeredVia: 'Triggered via %s',
  scheduled: 'Scheduled',
  commit: 'Commit',
  pushedBy: 'pushed by',
  rerun: 'Re-run',
  rerun_all: 'Re-run all jobs',
  rerun_failed: 'Re-run failed jobs',
  cancel: 'Cancel',
  approve: 'Approve',
  latest: 'Latest',
  latestAttempt: 'Latest attempt',
  attempt: 'Attempt',
  artifactsTitle: 'Artifacts',
  artifactExpired: 'Expired',
  artifactExpiresAt: 'Expires at %s',
  confirmDeleteArtifact: 'Delete artifact %s?',
  confirmDeleteRun: 'Delete run %s and all logs?',
  confirmClearRunHistory: 'Delete all run history and logs?',
  cancelAction: 'Cancel',
  deleteRun: 'Delete run',
  deleteArtifact: 'Delete artifact',
  clearHistory: 'Clear history',
  manageRuns: 'Manage',
  doneManaging: 'Done',
  expandCallerJobs: 'Expand child jobs',
  collapseCallerJobs: 'Collapse child jobs',
  show_timestamps: 'Show timestamps',
  show_log_seconds: 'Show seconds',
  show_full_screen: 'Show full screen',
  download_logs: 'Download logs',
  showTimeStamps: 'Show timestamps',
  showLogSeconds: 'Show seconds',
  showFullScreen: 'Show full screen',
  downloadLogs: 'Download logs',
  copyOutput: 'Copy output',
  logsAlwaysAutoScroll: 'Always auto-scroll',
  logsAlwaysExpandRunning: 'Always expand running steps',
  status_unknown: 'Unknown',
  status_waiting: 'Waiting',
  status_running: 'Running',
  status_success: 'Success',
  status_failure: 'Failure',
  status_cancelled: 'Cancelled',
  status_skipped: 'Skipped',
  status_blocked: 'Blocked',
};

const runs = ref<RunListItem[]>([]);
const selectedRunId = ref(0);
const selectedJobId = ref(0);
const selectedWorkflowId = ref('all');
const loading = ref(true);
const managementMode = ref(false);
const confirmDialog = ref<ConfirmDialogState | null>(null);
const confirmDialogEl = ref<HTMLElement | null>(null);
const toasts = ref<ToastMessage[]>([]);
let ws: WebSocket | null = null;
let resolveConfirm: ((confirmed: boolean) => void) | null = null;
let toastId = 0;

const selectedRun = computed(() => runs.value.find((run) => run.ID === selectedRunId.value));
const actionsViewUrl = computed(() => selectedRunId.value ? `/api/runs/${selectedRunId.value}` : '');
const totalJobs = computed(() => runs.value.reduce((sum, run) => sum + (run.Jobs?.length || 0), 0));
const activeRuns = computed(() => runs.value.filter((run) => normalizeStatus(run.Status) === 'running').length);
const isRunDetail = computed(() => selectedRunId.value > 0);

const workflowTabs = computed(() => buildWorkflowTabs(runs.value));

const filteredRuns = computed(() => {
  if (selectedWorkflowId.value === 'all') return runs.value;
  return runs.value.filter((run) => encodeWorkflowId(run.Workflow || 'local act workflow') === selectedWorkflowId.value);
});
const selectedWorkflow = computed(() => workflowTabs.value.find((workflow) => workflow.id === selectedWorkflowId.value));

function workflowName(run: RunListItem): string {
  return run.Workflow || 'local act workflow';
}

function runTitle(run: RunListItem): string {
  return run.Name || workflowName(run) || `Run #${run.ID}`;
}

function runJobCount(run: RunListItem): number {
  return run.Jobs?.length || 0;
}

function runHasJob(run: RunListItem, jobId: number): boolean {
  return Boolean(run.Jobs?.some((job) => job.ID === jobId));
}

function firstJobId(run: RunListItem): number {
  return run.Jobs?.[0]?.ID || 0;
}

function formatRunNumber(run: RunListItem): string {
  return `#${run.ID}`;
}

function notify(text: string, tone: ToastTone = 'info') {
  const id = ++toastId;
  toasts.value = [...toasts.value, {id, text, tone}];
  window.setTimeout(() => dismissToast(id), 5000);
}

function dismissToast(id: number) {
  toasts.value = toasts.value.filter((toast) => toast.id !== id);
}

function requestConfirm(options: ConfirmActionOptions): Promise<boolean> {
  if (resolveConfirm) {
    resolveConfirm(false);
  }
  return new Promise((resolve) => {
    resolveConfirm = resolve;
    confirmDialog.value = {
      title: options.title,
      message: options.message,
      confirmLabel: options.confirmLabel || 'Confirm',
      cancelLabel: options.cancelLabel || locale.cancelAction,
      tone: options.tone || 'default',
    };
    void nextTick(() => confirmDialogEl.value?.focus());
  });
}

function closeConfirm(confirmed: boolean) {
  if (!confirmDialog.value) return;
  const resolve = resolveConfirm;
  confirmDialog.value = null;
  resolveConfirm = null;
  resolve?.(confirmed);
}

function toggleManagementMode() {
  managementMode.value = !managementMode.value;
}

function parseLocation() {
  const match = window.location.pathname.match(/^\/runs\/(\d+)(?:\/jobs\/(\d+))?/);
  const params = new URLSearchParams(window.location.search);
  selectedRunId.value = match ? Number(match[1]) : 0;
  selectedJobId.value = match?.[2] ? Number(match[2]) : 0;
  selectedWorkflowId.value = params.get('workflow') || 'all';
}

function listQuery(): string {
  const params = new URLSearchParams();
  if (selectedWorkflowId.value !== 'all') params.set('workflow', selectedWorkflowId.value);
  const query = params.toString();
  return query ? `?${query}` : '';
}

function selectedWorkflowQuery(): string {
  if (selectedWorkflowId.value === 'all') return '';
  return `?workflow=${encodeURIComponent(selectedWorkflowId.value)}`;
}

function showList(replace = false) {
  selectedRunId.value = 0;
  selectedJobId.value = 0;
  const path = `/${listQuery()}`;
  const current = `${window.location.pathname}${window.location.search}`;
  if (current !== path) {
    window.history[replace ? 'replaceState' : 'pushState']({}, '', path);
  }
}

function selectRun(runId: number, jobId = 0, replace = false) {
  selectedRunId.value = runId;
  selectedJobId.value = jobId;
  const path = `${jobId ? `/runs/${runId}/jobs/${jobId}` : `/runs/${runId}`}${selectedWorkflowQuery()}`;
  const current = `${window.location.pathname}${window.location.search}`;
  if (current !== path) {
    window.history[replace ? 'replaceState' : 'pushState']({}, '', path);
  }
}

function selectWorkflow(workflowId: string) {
  selectedWorkflowId.value = workflowId;
  selectedRunId.value = 0;
  selectedJobId.value = 0;
  showList();
}

async function loadRuns() {
  const response = await fetch('/api/runs');
  runs.value = await response.json();
  if (runs.value.length === 0) {
    managementMode.value = false;
  }
  if (!workflowTabs.value.some((workflow) => workflow.id === selectedWorkflowId.value)) {
    selectedWorkflowId.value = 'all';
  }
  if (selectedRunId.value) {
    const currentRun = runs.value.find((run) => run.ID === selectedRunId.value);
    if (!currentRun) {
      showList(true);
    } else if (selectedJobId.value && !runHasJob(currentRun, selectedJobId.value)) {
      selectRun(currentRun.ID, firstJobId(currentRun), true);
    }
  }
  loading.value = false;
}

async function deleteRun(run: RunListItem) {
  if (!managementMode.value) return;
  const confirmed = await requestConfirm({
    title: locale.deleteRun,
    message: locale.confirmDeleteRun.replace('%s', formatRunNumber(run)),
    confirmLabel: locale.deleteRun,
    tone: 'danger',
  });
  if (!confirmed) return;
  const response = await fetch(`/api/runs/${run.ID}`, {method: 'DELETE'});
  if (!response.ok) {
    notify(`Failed to delete ${formatRunNumber(run)}`, 'error');
    return;
  }
  if (selectedRunId.value === run.ID) {
    showList(true);
  }
  await loadRuns();
}

async function clearRunHistory() {
  if (!managementMode.value) return;
  if (runs.value.length === 0) return;
  const confirmed = await requestConfirm({
    title: locale.clearHistory,
    message: locale.confirmClearRunHistory,
    confirmLabel: locale.clearHistory,
    tone: 'danger',
  });
  if (!confirmed) return;
  const response = await fetch('/api/runs', {method: 'DELETE'});
  if (!response.ok) {
    notify('Failed to clear run history', 'error');
    return;
  }
  managementMode.value = false;
  showList(true);
  await loadRuns();
}

function onRunCardKeydown(event: KeyboardEvent, run: RunListItem) {
  if ((event.target as HTMLElement).closest('button')) return;
  if (event.key !== 'Enter' && event.key !== ' ') return;
  event.preventDefault();
  selectRun(run.ID);
}

function openRunLink(event: MouseEvent) {
  const anchor = (event.target as HTMLElement).closest('a');
  const href = anchor?.getAttribute('href');
  if (!href?.startsWith('/runs/')) return;
  event.preventDefault();
  const match = href.match(/^\/runs\/(\d+)(?:\/jobs\/(\d+))?/);
  if (match) selectRun(Number(match[1]), match[2] ? Number(match[2]) : 0);
}

onMounted(async () => {
  parseLocation();
  await loadRuns();
  window.addEventListener('popstate', parseLocation);

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/ws`);
  ws.addEventListener('message', () => loadRuns());
});

onBeforeUnmount(() => {
  window.removeEventListener('popstate', parseLocation);
  closeConfirm(false);
  ws?.close();
});
</script>

<template>
  <div class="act-gui-shell" @click="openRunLink">
    <header class="act-gui-header">
      <div class="brand-block">
        <div class="brand-mark">
          <SvgIcon name="octicon-play" :size="18"/>
        </div>
        <div>
          <h1>act-gui</h1>
          <p>Local GitHub Actions runs from act</p>
        </div>
      </div>
      <div class="header-stats" aria-label="Run status">
        <span class="stat-pill">
          <SvgIcon name="octicon-repo" :size="14"/>
          localhost:27979
        </span>
        <span class="stat-pill strong">{{ runs.length }} runs</span>
        <span class="stat-pill">{{ totalJobs }} jobs</span>
        <span class="stat-pill live" :class="{active: activeRuns > 0}">
          {{ activeRuns }} running
        </span>
      </div>
    </header>

    <main class="act-gui-layout" :class="{detail: isRunDetail}">
      <section v-if="!isRunDetail" class="browser-view" aria-label="Actions browser">
        <section class="workflow-panel" aria-label="Workflows">
          <div class="panel-heading">
            <div>
              <p>Workflows</p>
              <h2>Workflows</h2>
            </div>
            <span>{{ Math.max(workflowTabs.length - 1, 0) }}</span>
          </div>
          <button
            v-for="workflow in workflowTabs"
            :key="workflow.id"
            type="button"
            class="workflow-card"
            :class="{selected: workflow.id === selectedWorkflowId}"
            @click="selectWorkflow(workflow.id)"
          >
            <span class="workflow-card-status">
              <ActionStatusIcon :status="workflow.latestStatus" :locale-status="locale.status[workflow.latestStatus]" icon-variant="circle-fill"/>
            </span>
            <span class="workflow-card-main">
              <span class="workflow-card-name">{{ workflow.name }}</span>
              <span class="workflow-card-meta">
                {{ workflow.count }} runs
              </span>
            </span>
            <SvgIcon name="octicon-chevron-right" :size="16" class="workflow-card-arrow"/>
          </button>
        </section>

        <section class="runs-panel" aria-label="Runs">
          <div class="runs-heading">
            <div>
              <p>Runs</p>
              <h2>{{ selectedWorkflow?.name || 'All workflows' }}</h2>
            </div>
            <div class="runs-heading-actions">
              <button
                v-if="selectedWorkflowId !== 'all'"
                type="button"
                class="ghost-button"
                @click="selectWorkflow('all')"
              >
                All workflows
              </button>
              <button
                v-if="runs.length > 0"
                type="button"
                class="ghost-button"
                :class="{active: managementMode}"
                @click="toggleManagementMode"
              >
                <SvgIcon :name="managementMode ? 'octicon-check' : 'octicon-gear'" :size="14"/>
                {{ managementMode ? locale.doneManaging : locale.manageRuns }}
              </button>
              <button
                v-if="managementMode && runs.length > 0"
                type="button"
                class="ghost-button danger"
                @click="clearRunHistory"
              >
                <SvgIcon name="octicon-trash" :size="14"/>
                Clear history
              </button>
            </div>
          </div>

          <div v-if="loading" class="empty-state">Loading runs...</div>
          <div v-else-if="filteredRuns.length === 0" class="empty-state">No runs yet</div>
          <div
            v-for="run in filteredRuns"
            :key="run.ID"
            class="run-card"
            role="button"
            tabindex="0"
            @click="selectRun(run.ID)"
            @keydown="onRunCardKeydown($event, run)"
          >
            <span class="run-card-status">
              <ActionStatusIcon :status="normalizeStatus(run.Status)" :locale-status="locale.status[normalizeStatus(run.Status)]" icon-variant="circle-fill"/>
            </span>
            <span class="run-card-main">
              <span class="run-card-title">{{ runTitle(run) }}</span>
              <span class="run-card-meta">
                <span>{{ formatRunNumber(run) }}</span>
                <span>{{ workflowName(run) }}</span>
                <span v-if="run.Branch">{{ run.Branch }}</span>
                <span v-if="run.CommitSHA">{{ run.CommitSHA }}</span>
                <span>{{ runJobCount(run) }} jobs</span>
              </span>
            </span>
            <span class="run-card-time">
              <SvgIcon name="octicon-clock" :size="12"/>
              <relative-time :datetime="run.CreatedAt" prefix=""/>
            </span>
            <button
              v-if="managementMode"
              type="button"
              class="run-card-action danger"
              :title="`Delete ${formatRunNumber(run)}`"
              :aria-label="`Delete ${formatRunNumber(run)}`"
              @click.stop="deleteRun(run)"
            >
              <SvgIcon name="octicon-trash" :size="14"/>
            </button>
            <SvgIcon name="octicon-chevron-right" :size="16" class="run-card-arrow"/>
          </div>
        </section>
      </section>

      <section v-else class="detail-screen" aria-label="Run detail screen">
        <div class="detail-toolbar">
          <div class="detail-toolbar-actions">
            <button type="button" class="back-button" @click="showList()">
              <SvgIcon name="octicon-chevron-left" :size="16"/>
              Runs
            </button>
            <button
              v-if="selectedRun"
              type="button"
              class="ghost-button"
              :class="{active: managementMode}"
              @click="toggleManagementMode"
            >
              <SvgIcon :name="managementMode ? 'octicon-check' : 'octicon-gear'" :size="14"/>
              {{ managementMode ? locale.doneManaging : locale.manageRuns }}
            </button>
            <button
              v-if="managementMode && selectedRun"
              type="button"
              class="ghost-button danger"
              @click="deleteRun(selectedRun)"
            >
              <SvgIcon name="octicon-trash" :size="14"/>
              Delete run
            </button>
          </div>
          <div v-if="selectedRun" class="detail-context">
            <strong>{{ runTitle(selectedRun) }}</strong>
            <span>{{ formatRunNumber(selectedRun) }} · {{ workflowName(selectedRun) }}</span>
          </div>
        </div>
        <section class="run-view" aria-label="Run details">
          <RepoActionView
            v-if="selectedRun && actionsViewUrl"
            :key="`${selectedRunId}-${selectedJobId}`"
            :job-id="selectedJobId"
            :actions-view-url="actionsViewUrl"
            :locale="locale"
            :management-mode="managementMode"
            :confirm-action="requestConfirm"
            :notify="notify"
          />
          <div v-else class="run-placeholder">
            Start an act run to see workflow jobs, steps, logs, and progress here.
          </div>
        </section>
      </section>
    </main>

    <div class="toast-stack" aria-live="polite" aria-atomic="true">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="act-toast"
        :class="toast.tone"
      >
        <SvgIcon :name="toast.tone === 'error' ? 'gitea-exclamation' : 'octicon-check'" :size="16"/>
        <span>{{ toast.text }}</span>
        <button type="button" class="toast-dismiss" :aria-label="`Dismiss ${toast.text}`" @click="dismissToast(toast.id)">
          <SvgIcon name="octicon-x" :size="14"/>
        </button>
      </div>
    </div>

    <div
      v-if="confirmDialog"
      class="confirm-backdrop"
      role="presentation"
      @click.self="closeConfirm(false)"
    >
      <section
        ref="confirmDialogEl"
        class="confirm-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        tabindex="-1"
        @keydown.esc="closeConfirm(false)"
      >
        <div class="confirm-dialog-icon" :class="confirmDialog.tone">
          <SvgIcon name="gitea-exclamation" :size="18"/>
        </div>
        <div class="confirm-dialog-body">
          <h2 id="confirm-dialog-title">{{ confirmDialog.title }}</h2>
          <p>{{ confirmDialog.message }}</p>
        </div>
        <div class="confirm-dialog-actions">
          <button type="button" class="ghost-button" @click="closeConfirm(false)">
            {{ confirmDialog.cancelLabel }}
          </button>
          <button
            type="button"
            class="confirm-button"
            :class="confirmDialog.tone"
            @click="closeConfirm(true)"
          >
            {{ confirmDialog.confirmLabel }}
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.act-gui-shell {
  --act-bg: #f4f6f8;
  --act-panel: #ffffff;
  --act-panel-subtle: #f8fafc;
  --act-ink: #1f2328;
  --act-muted: #656d76;
  --act-border: #d0d7de;
  --act-border-soft: #d8dee4;
  --act-blue: #0969da;
  --act-green: #1a7f37;
  --act-red: #cf222e;
  --act-shadow: 0 1px 2px rgba(31, 35, 40, 0.06);
  --color-body: var(--act-bg);
  --color-box-body: var(--act-panel);
  --color-box-header: var(--act-panel-subtle);
  --color-header-wrapper: var(--act-panel);
  --color-text: var(--act-ink);
  --color-text-light-1: #3f4750;
  --color-text-light-2: var(--act-muted);
  --color-secondary: var(--act-border-soft);
  --color-hover: #f1f6fd;
  --color-active: #ddf4ff;
  --color-primary: var(--act-blue);
  min-height: 100vh;
  background: var(--color-body);
  color: var(--color-text);
}

.act-gui-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  min-height: 64px;
  padding: 10px 18px;
  border-bottom: 1px solid var(--act-border);
  background: var(--color-header-wrapper);
  box-shadow: var(--act-shadow);
}

.brand-block {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.brand-mark {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border: 1px solid #8c959f;
  border-radius: 8px;
  color: #ffffff;
  background: #24292f;
}

.act-gui-header h1 {
  margin: 0;
  font-size: 20px;
  font-weight: var(--font-weight-semibold);
  line-height: 1.2;
  letter-spacing: 0;
}

.act-gui-header p {
  margin: 4px 0 0;
  color: var(--color-text-light-2);
  font-size: 13px;
}

.header-stats {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.stat-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 4px 9px;
  border: 1px solid var(--act-border-soft);
  border-radius: 999px;
  color: var(--act-muted);
  background: var(--act-panel-subtle);
  font-size: 12px;
  white-space: nowrap;
}

.stat-pill.strong {
  color: var(--act-ink);
  font-weight: 600;
}

.stat-pill.live.active {
  color: var(--act-green);
  border-color: #aceebb;
  background: #dafbe1;
}

.act-gui-layout {
  min-height: calc(100vh - 64px);
  background: var(--act-bg);
}

.empty-state,
.run-placeholder {
  padding: 20px;
  color: var(--color-text-light-2);
}

.browser-view {
  display: grid;
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr);
  align-items: start;
  gap: 18px;
  width: min(1180px, calc(100vw - 32px));
  margin: 0 auto;
  padding: 18px 0 24px;
}

.detail-screen {
  width: min(1180px, calc(100vw - 32px));
  margin: 0 auto;
  padding: 18px 0 24px;
}

.detail-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  min-height: 46px;
  margin-bottom: 14px;
}

.detail-toolbar-actions,
.runs-heading-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.workflow-panel,
.runs-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--act-border);
  border-radius: 8px;
  background: var(--act-panel);
}

.workflow-panel {
  max-height: calc(100vh - 112px);
  overflow-y: auto;
}

.runs-panel {
  max-height: calc(100vh - 112px);
  overflow-y: auto;
}

.panel-heading,
.runs-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin: 0 0 6px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--act-border-soft);
}

.panel-heading p,
.runs-heading p {
  margin: 0 0 3px;
  color: var(--act-muted);
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.panel-heading h2,
.runs-heading h2 {
  margin: 0;
  font-size: 20px;
  letter-spacing: 0;
}

.panel-heading span {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  min-height: 24px;
  padding: 2px 8px;
  border: 1px solid var(--act-border-soft);
  border-radius: 999px;
  color: var(--act-muted);
  background: var(--act-panel-subtle);
  font-size: 12px;
  font-weight: 700;
}

.workflow-card,
.run-card {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 66px;
  padding: 12px 14px;
  border: 1px solid var(--act-border);
  border-radius: 8px;
  background: var(--act-panel-subtle);
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.workflow-card:hover,
.run-card:hover {
  border-color: #54aeff;
  background: #f6f8fa;
}

.run-card:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
}

.workflow-card {
  min-height: 58px;
}

.workflow-card.selected {
  border-color: var(--act-blue);
  background: #ddf4ff;
  box-shadow: inset 3px 0 0 var(--act-blue);
}

.workflow-card-status,
.run-card-status {
  display: inline-flex;
  flex: 0 0 auto;
}

.workflow-card-main,
.run-card-main {
  display: flex;
  flex-direction: column;
  gap: 5px;
  flex: 1;
  min-width: 0;
}

.workflow-card-name,
.run-card-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 700;
}

.workflow-card-meta,
.run-card-meta,
.run-card-time {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-light-2);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workflow-card-arrow,
.run-card-arrow {
  color: var(--act-muted);
  flex: 0 0 auto;
}

.run-card-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: 30px;
  height: 30px;
  border: 1px solid transparent;
  border-radius: 7px;
  color: var(--color-text-light-2);
  background: transparent;
  cursor: pointer;
}

.run-card-action:hover {
  background: var(--color-hover);
  border-color: var(--act-border-soft);
}

.ghost-button,
.back-button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 32px;
  padding: 6px 10px;
  border: 1px solid var(--act-border);
  border-radius: 7px;
  color: var(--act-ink);
  background: var(--act-panel);
  font-weight: 600;
  cursor: pointer;
}

.ghost-button.danger,
.run-card-action.danger {
  color: var(--color-red);
}

.ghost-button.danger:hover,
.run-card-action.danger:hover {
  color: var(--color-red);
  border-color: var(--color-red-badge-bg);
  background: var(--color-red-badge-bg);
}

.ghost-button:hover,
.back-button:hover {
  background: #f6f8fa;
  border-color: #8c959f;
}

.ghost-button.active {
  color: var(--act-blue);
  border-color: var(--act-blue);
  background: #ddf4ff;
}

.detail-context {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  color: var(--act-muted);
  font-size: 12px;
}

.detail-context strong {
  color: var(--act-ink);
  font-size: 15px;
}

.run-view {
  min-width: 0;
  overflow: auto;
  padding: 14px 16px 18px;
  background: var(--act-bg);
}

.run-view :deep(.ui.fluid.container) {
  max-width: none !important;
}

.run-view :deep(.action-view-header) {
  margin-top: 0;
  padding: 14px;
  border: 1px solid var(--act-border);
  border-radius: 8px 8px 0 0;
  background: var(--act-panel);
}

.run-view :deep(.action-view-body) {
  margin: 0;
  padding-top: 0;
  gap: 0;
}

.run-view :deep(.action-view-left) {
  width: 280px;
  max-width: 280px;
  padding: 12px;
  border: 1px solid var(--act-border);
  border-top: 0;
  border-radius: 0 0 0 8px;
  background: var(--act-panel);
}

.run-view :deep(.action-view-right) {
  width: calc(100% - 280px);
  border-color: var(--act-border);
  border-top: 0;
  border-left: 0;
  border-radius: 0 0 8px 0;
  min-height: calc(100vh - 160px);
}

.toast-stack {
  position: fixed;
  top: 76px;
  right: 18px;
  z-index: 1001;
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: min(360px, calc(100vw - 32px));
}

.act-toast {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 42px;
  padding: 10px 12px;
  border: 1px solid var(--act-border);
  border-radius: 8px;
  background: var(--act-panel);
  box-shadow: 0 8px 24px rgba(31, 35, 40, 0.14);
  color: var(--act-ink);
}

.act-toast.error {
  border-color: #ff818266;
  color: var(--act-red);
  background: #fff8f8;
}

.act-toast.success {
  border-color: #aceebb;
  color: var(--act-green);
  background: #f0fff4;
}

.act-toast > span {
  flex: 1;
  min-width: 0;
}

.toast-dismiss {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: 1px solid transparent;
  border-radius: 7px;
  color: inherit;
  background: transparent;
  cursor: pointer;
}

.toast-dismiss:hover {
  border-color: var(--act-border-soft);
  background: #f6f8fa;
}

.confirm-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(31, 35, 40, 0.42);
}

.confirm-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  width: min(440px, calc(100vw - 32px));
  padding: 18px;
  border: 1px solid var(--act-border);
  border-radius: 8px;
  background: var(--act-panel);
  box-shadow: 0 16px 48px rgba(31, 35, 40, 0.22);
  outline: none;
}

.confirm-dialog-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border: 1px solid var(--act-border-soft);
  border-radius: 8px;
  color: var(--act-muted);
  background: var(--act-panel-subtle);
}

.confirm-dialog-icon.danger {
  color: var(--act-red);
  border-color: #ff818266;
  background: #fff8f8;
}

.confirm-dialog-body {
  min-width: 0;
}

.confirm-dialog-body h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.3;
  letter-spacing: 0;
}

.confirm-dialog-body p {
  margin: 8px 0 0;
  color: var(--act-muted);
  line-height: 1.45;
}

.confirm-dialog-actions {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 4px;
}

.confirm-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 32px;
  padding: 6px 12px;
  border: 1px solid var(--act-blue);
  border-radius: 7px;
  color: #ffffff;
  background: var(--act-blue);
  font-weight: 600;
  cursor: pointer;
}

.confirm-button.danger {
  border-color: var(--act-red);
  background: var(--act-red);
}

.confirm-button:hover {
  filter: brightness(0.96);
}

@media (max-width: 900px) {
  .browser-view {
    grid-template-columns: 1fr;
    width: calc(100vw - 20px);
    padding-top: 10px;
  }

  .detail-screen {
    width: calc(100vw - 20px);
    padding-top: 10px;
  }

  .workflow-panel,
  .runs-panel {
    max-height: none;
    overflow: visible;
  }

  .run-view {
    padding: 10px;
  }

  .run-view :deep(.action-view-body) {
    flex-direction: column;
  }

  .run-view :deep(.action-view-left),
  .run-view :deep(.action-view-right) {
    width: 100%;
    max-width: none;
    border: 1px solid var(--act-border);
    border-radius: 0;
  }
}

@media (max-width: 640px) {
  .act-gui-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .header-stats {
    justify-content: flex-start;
  }

  .detail-toolbar,
  .panel-heading,
  .runs-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .run-card {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .run-card-time {
    width: 100%;
    padding-left: 26px;
  }
}
</style>
