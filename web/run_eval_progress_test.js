'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const bundle = fs.readFileSync(process.argv[2], 'utf8');
function source(name) {
  const start = bundle.search(new RegExp('^(?:async )?function ' + name + '\\(', 'm'));
  assert.ok(start >= 0, `${name} must exist`);
  const rest = bundle.slice(start);
  const next = /\n(?:async )?function \w+\(/.exec(rest);
  return next ? rest.slice(0, next.index) : rest;
}
const pane = {innerHTML: ''};
const ctx = {
  state: {runEvalThoughts: [], runEvalBusy: true},
  escapeHTML: value => String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
  $: () => ({querySelector: () => pane}),
  runEvalModel: () => 'codex:custom-model',
  EventSource: class {
    constructor() { this.listeners = {}; }
    addEventListener(name, listener) { this.listeners[name] = listener; }
    close() {}
  }
};
vm.createContext(ctx);
for (const name of ['defaultRunEval', 'runEvalThoughtsHTML', 'appendRunEvalThought', 'runEvalPaneHTML', 'attachRunEvalStream']) {
  vm.runInContext(source(name), ctx);
}
ctx.attachRunEvalStream('r1');
const thought = payload => ctx.state.runEvalStream.listeners.thought({data: JSON.stringify(payload)});
thought({kind: 'status', text: '正在整理轨迹…'});
thought({kind: 'thinkingStart'});
for (const text of ['核对', ' 2026', ' 年\n', '和证据']) thought({kind: 'thinking', text});
assert.equal(ctx.state.runEvalThoughts.length, 2, 'chunks form one reasoning block');
assert.equal(ctx.state.runEvalThoughts[1].text, '核对 2026 年\n和证据', 'preserve spaces and newlines');
assert.equal((pane.innerHTML.match(/class="thought-item"/g) || []).length, 2);
assert.ok(!pane.innerHTML.includes('<p>'), 'live updates use the same compact markup as initial rendering');
thought({kind: 'thinking', text: '核对 2026 年\n和证据', full: true});
assert.equal(ctx.state.runEvalThoughts[1].text, '核对 2026 年\n和证据', 'full blocks replace deltas');
thought({kind: 'thinkingStart'});
thought({kind: 'thinking', text: '<新的分析>'});
assert.equal(ctx.state.runEvalThoughts.length, 3, 'new reasoning block keeps its boundary');
assert.ok(pane.innerHTML.includes('&lt;新的分析&gt;'));
const beforeJSON = pane.innerHTML;
for (const text of ['{"score":', '66', '.', '4}']) thought({kind: 'answer', text});
assert.equal(pane.innerHTML, beforeJSON, 'raw report JSON must not appear as progress');
const runningHTML = ctx.runEvalPaneHTML({id: 'r1', status: 'succeeded'});
assert.ok(runningHTML.includes(pane.innerHTML), 'initial and live progress rendering stay consistent');
ctx.state.runEvalBusy = false;
ctx.state.runEval = {runId: 'r1', status: 'failed', error: 'invalid eval JSON'};
const failedHTML = ctx.runEvalPaneHTML({id: 'r1', status: 'succeeded'});
assert.ok(failedHTML.includes('invalid eval JSON'));
assert.ok(!failedHTML.includes('API Key') && !failedHTML.includes('网络状态'), 'format failures must not be blamed on credentials or networking');
console.log('run eval progress ok');
