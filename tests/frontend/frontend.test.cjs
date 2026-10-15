'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {test} = require('node:test');

// A small DOM/event harness for the dependency-free frontend. In particular,
// disabled fieldsets prevent user interaction with all descendant controls.
class Element {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.listeners = {};
    this.value = '';
    this.textContent = '';
    this.disabled = false;
    this.checked = false;
  }
  set value(value) { this._value = String(value); }
  get value() { return this._value; }
  append(...children) {
    for (const child of children) {
      child.parent = this;
      this.children.push(child);
    }
  }
  replaceChildren(...children) {
    this.children.forEach(child => { child.parent = null; });
    this.children = [];
    this.append(...children);
  }
  addEventListener(event, listener) { this.listeners[event] = listener; }
  setAttribute(name, value) { this[name] = value; }
}
const walk = root => [root, ...root.children.flatMap(walk)];
const text = root => root.textContent + root.children.map(text).join('');
const findButton = (root, label) => walk(root).find(node => node.tag === 'button' && text(node) === label);
function disabled(node) {
  if (node.disabled) return true;
  for (let parent = node.parent; parent; parent = parent.parent) {
    if (parent.tag === 'fieldset' && parent.disabled) return true;
  }
  return false;
}
function click(node) {
  assert.ok(node, 'button exists');
  return disabled(node) ? Promise.resolve() : node.listeners.click({currentTarget:node});
}
function change(node, value, event = 'input') {
  assert.ok(node, 'input exists');
  if (disabled(node)) return;
  if (node.type === 'checkbox' || node.type === 'radio') node.checked = value;
  else node.value = value;
  node.listeners[event]({target:node});
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
}
const flush = () => new Promise(resolve => setImmediate(resolve));
const clone = value => JSON.parse(JSON.stringify(value));
async function browser(handler) {
  const nodes = {'#app':new Element('main'), '#nav':new Element('nav'), '#notice':new Element('aside')};
  const calls = [];
  const context = vm.createContext({
    Node:Element,
    document:{
      querySelector:selector => nodes[selector], querySelectorAll:() => [],
      createElement:tag => new Element(tag),
      createTextNode:value => Object.assign(new Element('#text'), {textContent:String(value)})
    },
    window:{addEventListener() {}, scrollTo() {}},
    location:{pathname:'/login', search:'', origin:'http://localhost'},
    history:{pushState(_state, _unused, url) { context.location.pathname = url; }},
    navigator:{}, URLSearchParams, setTimeout() {}, clearTimeout() {},
    async fetch(url, options) {
      const request = {url, method:options.method, body:options.body === undefined ? undefined : JSON.parse(options.body)};
      calls.push(request);
      const data = url === '/api/me' ? {username:'owner'} : await handler(request);
      return {ok:true, json:async () => clone(data)};
    }
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../../internal/web/static/app.js'), 'utf8'), context);
  await flush();
  return {calls, nodes, context, view:name => vm.runInContext(`${name}('s')`, context)};
}
function draft() {
  return {id:'s', revision:1, title:'Original title', description:'Description', status:'draft', allowAnonymous:true,
    allowRepeat:false, deadline:null, questions:[{id:'q', title:'Question', type:'multiple', required:true,
      options:[{id:'one', label:'One'}, {id:'two', label:'Two'}], validation:{minSelect:1},
      rules:[{operator:'contains', value:'one', target:'END'}]}]};
}
function titleField(root) { return walk(root).find(node => node.tag === 'input' && node.type === 'text'); }
function assertEditorLocked(root) {
  const controls = walk(root).filter(node => ['input', 'textarea', 'select', 'button'].includes(node.tag));
  assert.ok(controls.length > 10, 'settings, questions, options, rules and actions are covered');
  for (const control of controls) assert.equal(disabled(control), true, `${control.tag} ${text(control) || control.value} must be disabled`);
}

for (const id of ['__proto__', 'constructor', 'toString']) {
  for (const type of ['text', 'number', 'single', 'multiple']) {
    test(`${type} answers survive forward/back navigation and JSON submission with ID ${id}`, async () => {
      const value = {text:'A real answer', number:42, single:'one', multiple:['one']}[type];
      const survey = {...draft(), status:'published', questions:[{id, title:'Special question', type, required:true,
        options:[{id:'one', label:'One'}, {id:'two', label:'Two'}], validation:{}}]};
      const ui = await browser(({url, body}) => {
        if (url.endsWith('/next')) return {answer:body.answer, nextQuestionId:'END'};
        if (url.endsWith('/responses')) return {};
        return survey;
      });
      const root = await ui.view('fillView');
      const control = walk(root).find(node => node.tag === (type === 'text' ? 'textarea' : 'input'));
      if (type === 'text' || type === 'number') {
        assert.equal(control.value, '', 'prototype members must not appear as initial answers');
        change(control, String(value));
      } else {
        assert.equal(control.checked, false);
        change(control, true, 'change');
      }
      await click(findButton(root, '下一步'));
      assert.deepEqual(ui.calls.find(call => call.url.endsWith('/next')).body.answer, value);
      await click(findButton(root, '上一步'));
      const restored = walk(root).find(node => node.tag === (type === 'text' ? 'textarea' : 'input'));
      if (type === 'text' || type === 'number') assert.equal(restored.value, String(value));
      else assert.equal(restored.checked, true);
      await click(findButton(root, '下一步'));
      await click(findButton(root, '提交问卷'));
      const answers = ui.calls.find(call => call.url.endsWith('/responses')).body.answers;
      assert.deepEqual(Object.keys(answers), [id]);
      assert.deepEqual(answers[id], value);
      assert.match(text(root), /提交成功/);
    });
  }
}

test('saving locks every editor control, preserves entered content and blocks concurrent requests', async () => {
  const pending = deferred();
  const ui = await browser(request => request.method === 'PUT' ? pending.promise : draft());
  const root = await ui.view('editorView');
  change(titleField(root), 'My edited title');
  const saving = click(findButton(root, '保存草稿'));
  assertEditorLocked(root);
  change(titleField(root), 'Attempted input during save');
  await click(findButton(root, '保存并发布'));
  await click(findButton(root, '＋ 添加题目'));
  assert.equal(ui.calls.filter(call => call.method === 'PUT').length, 1);
  assert.equal(ui.calls.filter(call => call.url.endsWith('/publish')).length, 0);
  pending.resolve({...ui.calls.find(call => call.method === 'PUT').body, revision:2});
  await saving;
  assert.equal(titleField(root).value, 'My edited title');
  assert.equal(disabled(titleField(root)), false);
  assert.match(text(ui.nodes['#notice']), /草稿已保存/);
});

for (const failure of ['save', 'publish']) {
  test(`${failure} failure unlocks the editor and retains changes for retry`, async () => {
    const pending = deferred();
    let fail = true;
    const ui = await browser(request => {
      if (request.method === 'PUT') return failure === 'save' && fail ? pending.promise : {...request.body, revision:2};
      if (request.url.endsWith('/publish')) return fail ? pending.promise : {...draft(), status:'published'};
      return draft();
    });
    const root = await ui.view('editorView');
    change(titleField(root), 'Keep this edit');
    const saving = click(findButton(root, failure === 'save' ? '保存草稿' : '保存并发布'));
    await flush();
    assertEditorLocked(root);
    pending.reject(new Error('Temporary failure'));
    await saving;
    assert.equal(titleField(root).value, 'Keep this edit');
    assert.equal(disabled(titleField(root)), false);
    assert.match(text(ui.nodes['#notice']), /Temporary failure/);
    fail = false;
    await click(findButton(root, '保存草稿'));
    assert.equal(ui.calls.filter(call => call.method === 'PUT').at(-1).body.title, 'Keep this edit');
    assert.match(text(ui.nodes['#notice']), /草稿已保存/);
  });
}

test('publishing keeps the entire editor locked between saving and publishing', async () => {
  const saved = deferred(), published = deferred();
  const ui = await browser(request => {
    if (request.method === 'PUT') return saved.promise;
    if (request.url.endsWith('/publish')) return published.promise;
    return draft();
  });
  const root = await ui.view('editorView');
  const publishing = click(findButton(root, '保存并发布'));
  assertEditorLocked(root);
  saved.resolve({...draft(), revision:2});
  await flush();
  assertEditorLocked(root);
  await click(findButton(root, '保存草稿'));
  assert.equal(ui.calls.filter(call => call.method === 'PUT').length, 1);
  published.resolve({...draft(), status:'published', revision:3});
  await publishing;
  assert.match(text(root), /已发布/);
  assert.equal(findButton(root, '保存草稿'), undefined);
});

function statistics() {
  return {totalResponses:21, questions:[
    {id:'text & special', title:'Text answers', type:'text', answered:21, values:Array.from({length:20}, (_, i) => `Answer ${i+1}`), valuesOffset:0, valuesLimit:20, hasMore:true},
    {id:'number', title:'Number answers', type:'number', answered:21, average:11, values:Array.from({length:20}, (_, i) => i+1), valuesOffset:0, valuesLimit:20, hasMore:true},
    {id:'choice', title:'Choice answers', type:'single', answered:21, options:[{id:'one', label:'One', count:21}]}
  ]};
}
function card(root, title) { return walk(root).find(node => node.tag === 'section' && node.children.some(child => text(child).includes(title)) && walk(node).some(child => child.tag === 'h2' && text(child) === title)); }

test('statistics page raw text and numbers independently, keeping complete totals, choices and averages', async () => {
  const pending = deferred();
  let waitForPage = true;
  const ui = await browser(request => {
    if (request.url.includes('/stats?')) {
      if (waitForPage) return pending.promise;
      const params = new URLSearchParams(request.url.split('?')[1]);
      const question = statistics().questions.find(q => q.id === params.get('questionId'));
      return {totalResponses:21, questions:[{...question, valuesOffset:Number(params.get('offset')),
        values:params.get('offset') === '0' ? question.values : [21], hasMore:params.get('offset') === '0'}]};
    }
    return request.url.endsWith('/stats') ? statistics() : draft();
  });
  const root = await ui.view('statsView');
  const textCard = card(root, 'Text answers'), numberCard = card(root, 'Number answers'), choiceCard = card(root, 'Choice answers');
  assert.ok(disabled(findButton(textCard, '上一页')));
  const page = click(findButton(textCard, '下一页'));
  assert.ok(disabled(findButton(textCard, '下一页')));
  assert.ok(disabled(findButton(textCard, '上一页')));
  await click(findButton(textCard, '下一页'));
  assert.equal(ui.calls.filter(call => call.url.includes('/stats?')).length, 1);
  const params = new URLSearchParams(ui.calls.at(-1).url.split('?')[1]);
  assert.equal(params.get('questionId'), 'text & special');
  assert.equal(params.get('offset'), '20');
  assert.equal(params.get('limit'), '20');
  pending.resolve({totalResponses:21, questions:[{...statistics().questions[0], values:['Final answer'], valuesOffset:20, hasMore:false}]});
  await page;
  assert.match(text(textCard), /Final answer/);
  assert.doesNotMatch(text(textCard), /Answer 1/);
  assert.equal(walk(textCard).find(node => node.tag === 'ol').start, 21);
  assert.ok(disabled(findButton(textCard, '下一页')));
  assert.equal(card(root, 'Number answers'), numberCard, 'other questions retain their DOM and page');
  assert.equal(card(root, 'Choice answers'), choiceCard);
  assert.match(text(numberCard), /平均值：11/);
  assert.match(text(choiceCard), /21 次选择 · 100%/);
  waitForPage = false;
  await click(findButton(textCard, '上一页'));
  assert.match(text(textCard), /Answer 1/);
  await click(findButton(numberCard, '下一页'));
  assert.match(text(numberCard), /平均值：11/);
  assert.match(text(numberCard), /21 份回答/);
  assert.deepEqual(walk(numberCard).filter(node => node.tag === 'li').map(text), ['21']);
});

test('statistics pagination refreshes returned summaries when new submissions arrive', async () => {
  const pending = deferred();
  const ui = await browser(request => {
    if (request.url.includes('/stats?')) return pending.promise;
    return request.url.endsWith('/stats') ? statistics() : draft();
  });
  const root = await ui.view('statsView');
  const numberCard = card(root, 'Number answers'), choiceCard = card(root, 'Choice answers');
  const total = walk(root).find(node => node.className === 'large-number');
  const originalChoices = text(choiceCard);
  const paging = click(findButton(numberCard, '下一页'));
  assert.equal(text(total), '21', 'keep the previous total while the request is pending');
  assert.match(text(numberCard), /平均值：11/);
  pending.resolve({totalResponses:22, questions:[{...statistics().questions[1], answered:22,
    average:15, values:[21, 99], valuesOffset:20, hasMore:false}]});
  await paging;
  assert.match(text(numberCard), /平均值：15/);
  assert.match(text(numberCard), /22 份回答/);
  assert.equal(text(total), '22');
  assert.deepEqual(walk(numberCard).filter(node => node.tag === 'li').map(text), ['21', '99']);
  assert.equal(text(choiceCard), originalChoices, 'do not fabricate refreshed summaries for other questions');
  assert.match(text(card(root, 'Text answers')), /21 份回答/);
});

test('failed statistics pagination retains the current page and allows retry', async () => {
  let fail = true;
  const ui = await browser(request => {
    if (request.url.includes('/stats?')) {
      if (fail) throw new Error('Page unavailable');
      return {totalResponses:21, questions:[{...statistics().questions[0], values:['Last'], valuesOffset:20, hasMore:false}]};
    }
    return request.url.endsWith('/stats') ? statistics() : draft();
  });
  const root = await ui.view('statsView');
  const question = card(root, 'Text answers');
  await click(findButton(question, '下一页'));
  assert.match(text(question), /Answer 1/);
  assert.equal(text(walk(root).find(node => node.className === 'large-number')), '21');
  assert.match(text(ui.nodes['#notice']), /Page unavailable/);
  assert.equal(disabled(findButton(question, '下一页')), false);
  fail = false;
  await click(findButton(question, '下一页'));
  assert.match(text(question), /Last/);
});

test('switching question filters during pagination refreshes the visible card and releases its controls', async () => {
  const pending = deferred();
  const ui = await browser(request => {
    if (request.url.includes('/stats?')) return pending.promise;
    return request.url.endsWith('/stats') ? statistics() : draft();
  });
  const root = await ui.view('statsView');
  const paging = click(findButton(card(root, 'Text answers'), '下一页'));
  const filter = walk(root).find(node => node.tag === 'select');
  change(filter, 'number', 'change');
  assert.equal(card(root, 'Text answers'), undefined);
  change(filter, 'text & special', 'change');
  const visible = card(root, 'Text answers');
  assert.ok(disabled(findButton(visible, '下一页')));
  pending.resolve({totalResponses:21, questions:[{...statistics().questions[0], values:['Last'], valuesOffset:20, hasMore:false}]});
  await paging;
  assert.match(text(visible), /Last/);
  assert.equal(disabled(findButton(visible, '上一页')), false);
});

for (const outcome of ['resolve', 'reject']) {
  test(`statistics ignores a stale ${outcome} after navigation`, async () => {
    const pending = deferred();
    const ui = await browser(request => {
      if (request.url.includes('/stats?')) return pending.promise;
      return request.url.endsWith('/stats') ? statistics() : draft();
    });
    const root = await ui.view('statsView');
    ui.nodes['#app'].replaceChildren(root);
    const paging = click(findButton(card(root, 'Text answers'), '下一页'));
    vm.runInContext("go('/login')", ui.context);
    await flush();
    const current = text(ui.nodes['#app']);
    if (outcome === 'resolve') pending.resolve({totalResponses:22, questions:[{...statistics().questions[0], values:['Stale value'], valuesOffset:20, hasMore:false}]});
    else pending.reject(new Error('Stale error'));
    await paging;
    assert.equal(text(ui.nodes['#app']), current);
    assert.equal(ui.nodes['#notice'].hidden, true);
    assert.doesNotMatch(text(root), /Stale value/);
    assert.equal(text(walk(root).find(node => node.className === 'large-number')), '21');
  });
}
