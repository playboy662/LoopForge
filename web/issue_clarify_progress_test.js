'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const bundle = fs.readFileSync(process.argv[2], 'utf8');
function source(name) {
  const start = bundle.search(new RegExp('^(?:async )?function ' + name + '\\(', 'm'));
  assert.ok(start >= 0, name);
  const rest = bundle.slice(start);
  const next = /\n(?:async )?function \w+\(/.exec(rest);
  return next ? rest.slice(0, next.index) : rest;
}
const box = {hidden: true, innerHTML: '', scrollHeight: 100};
const jobs = new Map([['i1', {thinking: [], lastSeq: 0}]]);
const ctx = {
  state: {issueDetailData: {issue: {id: 'i1'}}},
  issueClarifyJobs: jobs,
  $: selector => selector === '#issueClarifyThinking' ? box : null,
  TextDecoder,
  escapeHTML: value => String(value ?? '').replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]))
};
vm.createContext(ctx);
for (const name of ['parseInlineMarkdown', 'renderMarkdown', 'clarifyThoughtShouldHide', 'clarifyToolLabel', 'mergeClarifyThought', 'renderIssueClarifyThoughts', 'appendIssueClarifyThought', 'issueClarifyThinking', 'restoreIssueClarifyThinking', 'consumeIssueClarifyStream']) {
  vm.runInContext(source(name), ctx);
}
const feed = event => ctx.issueClarifyThinking(event, 'i1');
feed({kind: 'status', text: '正在准备分析…'});
feed({kind: 'thinkingStart'});
for (const text of ['**规划仓库分析**\n', '我', '先', '读取', ' 原单', '，并检查', ' 两个仓库。']) feed({kind: 'thinking', text});
assert.equal(jobs.get('i1').thinking.length, 2, 'tokens accumulate into one block');
assert.ok(box.innerHTML.includes('我先读取 原单，并检查 两个仓库。'));
assert.ok(box.innerHTML.includes('<strong>规划仓库分析</strong>'));
assert.equal((box.innerHTML.match(/think-icon/g) || []).length, 2);
feed({kind: 'tool', id: 'read-1', tool: 'clarify_repository_read'});
feed({kind: 'tool', id: 'read-1', tool: 'clarify_repository_read', detail: 'backend/src/auth.go'});
feed({kind: 'toolDone', id: 'read-1'});
feed({kind: 'tool', id: 'read-2', tool: 'clarify_repository_read', detail: 'frontend/src/login.vue'});
feed({kind: 'tool', id: 'read-3', tool: 'clarify_repository_read', detail: 'backend/src/auth.go'});
assert.equal(jobs.get('i1').thinking.filter(item => item.kind === 'tool').length, 3, 'only identical call IDs merge; distinct reads remain');
assert.ok(box.innerHTML.includes('读取仓库 · backend/src/auth.go'));
assert.ok(box.innerHTML.includes('读取仓库 · frontend/src/login.vue'));
feed({kind: 'answer', text: '{"summary":'});
feed({kind: 'answer', text: '"private protocol","questions":[]}'});
assert.ok(!box.innerHTML.includes('private protocol'), 'JSON remains hidden across deltas');
const beforeRestore = box.innerHTML;
box.innerHTML = '';
ctx.restoreIssueClarifyThinking('i1');
assert.equal(box.innerHTML, beforeRestore, 'reopening renders compact saved blocks');
feed({kind: 'thinkingStart'});
feed({kind: 'thinking', text: '新的分析', full: true});
feed({kind: 'thinking', text: '新的分析', full: true});
assert.equal(jobs.get('i1').thinking.at(-1).text, '新的分析');
assert.ok(!box.innerHTML.includes('新的分析新的分析'));
ctx.state.issueDetailData.issue.id = 'other';
const otherHTML = box.innerHTML;
feed({kind: 'status', text: '后台继续'});
assert.equal(box.innerHTML, otherHTML, 'background issue does not alter a different drawer');
ctx.state.issueDetailData.issue.id = 'i1';

(async () => {
  const wire = 'id: 21\nevent: thought\ndata: {"kind":"thinkingStart"}\n\n' +
    'id: 22\nevent: thought\ndata: {"kind":"thinking","text":"续接"}\n\n' +
    'id: 22\nevent: thought\ndata: {"kind":"thinking","text":"续接"}\n\n' +
    'id: 23\nevent: thought\ndata: {"kind":"thinking","text":"内容"}\n\n' +
    'event: done\ndata: {"parsed":{"ready":true}}\n\n';
  const bytes = new TextEncoder().encode(wire);
  let offset = 0;
  const response = {body: {getReader: () => ({read: async () => {
    if (offset >= bytes.length) return {done: true};
    const value = bytes.slice(offset, offset += 7);
    return {done: false, value};
  }})}};
  const result = await ctx.consumeIssueClarifyStream(response, 'i1');
  assert.equal(result.parsed.ready, true);
  assert.equal(jobs.get('i1').lastSeq, 23);
  assert.equal(jobs.get('i1').thinking.at(-1).text, '续接内容', 'SSE replay IDs prevent repeated deltas across UTF-8 chunks');
  jobs.get('i1').startedAt='old-process';
  const restarted=new TextEncoder().encode('event: job\ndata: {"startedAt":"new-process"}\n\nid: 1\nevent: thought\ndata: {"kind":"thinking","text":"恢复后的完整段落","full":true}\n\nevent: done\ndata: {}\n\n');
  let sent=false;
  await ctx.consumeIssueClarifyStream({body:{getReader:()=>({read:async()=>sent?{done:true}:(sent=true,{done:false,value:restarted})})}},'i1');
  assert.equal(jobs.get('i1').thinking.length,1);
  assert.equal(jobs.get('i1').thinking[0].text,'恢复后的完整段落','server restarts reset the sequence and replace old progress');
  const items = [];
  for (let i = 0; i < 200; i++) ctx.mergeClarifyThought(items, {kind:'status', text:`step ${i}`});
  assert.equal(items.length, 80, 'history remains bounded');
  console.log('issue clarify progress ok');
})().catch(error => {console.error(error);process.exit(1)});
