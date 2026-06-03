export type ActionStatus = 'unknown' | 'waiting' | 'running' | 'success' | 'failure' | 'cancelled' | 'skipped' | 'blocked';

export type RunListItem = {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  Name: string;
  Workflow: string;
  Event: string;
  Branch: string;
  CommitSHA: string;
  Status: ActionStatus;
  Jobs?: Array<{ID: number; Status: string}>;
};

export type WorkflowTab = {
  id: string;
  name: string;
  count: number;
  latestStatus: ActionStatus;
};

const actionStatuses = new Set<ActionStatus>([
  'unknown',
  'waiting',
  'running',
  'success',
  'failure',
  'cancelled',
  'skipped',
  'blocked',
]);

export function normalizeStatus(status: string): ActionStatus {
  if (status === 'failed') return 'failure';
  if (actionStatuses.has(status as ActionStatus)) return status as ActionStatus;
  return 'unknown';
}

export function encodeWorkflowId(workflow: string): string {
  return workflow;
}

export function buildWorkflowTabs(runs: RunListItem[]): WorkflowTab[] {
  const groups = new Map<string, {name: string; runs: RunListItem[]}>();
  for (const run of runs) {
    const name = run.Workflow || 'local act workflow';
    const id = encodeWorkflowId(name);
    const group = groups.get(id) || {name, runs: []};
    group.runs.push(run);
    groups.set(id, group);
  }
  const tabs = [...groups.entries()]
    .map(([id, group]) => ({
      id,
      name: group.name,
      count: group.runs.length,
      latestStatus: normalizeStatus(group.runs[0]?.Status || 'waiting'),
    }))
    .sort((a, b) => a.name.localeCompare(b.name));
  const activeRuns = runs.filter((run) => normalizeStatus(run.Status) === 'running').length;
  return [{
    id: 'all',
    name: 'All workflows',
    count: runs.length,
    latestStatus: runs.length === 0 ? 'waiting' : activeRuns > 0 ? 'running' : normalizeStatus(runs[0].Status),
  }, ...tabs];
}
