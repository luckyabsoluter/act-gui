import assert from 'node:assert/strict';
import {buildWorkflowTabs, normalizeStatus, type RunListItem} from './workflow-tabs.ts';

function run(status: RunListItem['Status'], workflow = '.github/workflows/test.yml'): RunListItem {
  return {
    ID: 1,
    CreatedAt: '',
    UpdatedAt: '',
    Name: 'act push',
    Workflow: workflow,
    Event: 'push',
    Branch: 'main',
    CommitSHA: 'abc1234',
    Status: status,
  };
}

const emptyTabs = buildWorkflowTabs([]);
assert.equal(emptyTabs.length, 1);
assert.equal(emptyTabs[0].name, 'All workflows');
assert.equal(emptyTabs[0].count, 0);
assert.equal(emptyTabs[0].latestStatus, 'waiting');

const runningTabs = buildWorkflowTabs([run('success'), run('running')]);
assert.equal(runningTabs[0].latestStatus, 'running');

assert.equal(normalizeStatus('failed'), 'failure');
assert.equal(normalizeStatus('not-a-status'), 'unknown');
