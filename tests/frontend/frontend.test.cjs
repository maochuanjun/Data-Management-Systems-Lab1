"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const {test} = require("node:test");

// A small DOM/event harness for the dependency-free frontend. In particular,
// disabled fieldsets prevent user interaction with all descendant controls.
class Element {
  constructor(tag) {
    this.tag = tag;
    this.tagName = tag.toUpperCase();
    this.children = [];
    this.listeners = {};
    this.value = "";
    this.textContent = "";
    this.disabled = false;
    this.checked = false;
  }
  set value(value) {
    this._value = String(value);
  }
  get value() {
    return this._value;
  }
  append(...children) {
    for (const child of children) {
      child.parent = this;
      this.children.push(child);
    }
  }
  replaceChildren(...children) {
    this.children.forEach((child) => {
      child.parent = null;
    });
    this.children = [];
    this.append(...children);
  }
  querySelectorAll(selector) {
    const tags = selector.split(",");
    return walk(this).slice(1).filter((node) => tags.includes(node.tag));
  }
  addEventListener(event, listener) {
    this.listeners[event] = listener;
  }
  setAttribute(name, value) {
    this[name] = value;
  }
}
const walk = (root) => [root, ...root.children.flatMap(walk)];
const text = (root) => root.textContent + root.children.map(text).join("");
const findButton = (root, label) =>
  walk(root).find((node) => node.tag === "button" && text(node) === label);
function disabled(node) {
  if (node.disabled) return true;
  for (let parent = node.parent; parent; parent = parent.parent) {
    if (parent.tag === "fieldset" && parent.disabled) return true;
  }
  return false;
}
function click(node) {
  assert.ok(node, "button exists");
  return disabled(node)
    ? Promise.resolve()
    : node.listeners.click({currentTarget: node});
}
function change(node, value, event = "input") {
  assert.ok(node, "input exists");
  if (disabled(node)) return;
  if (node.type === "checkbox" || node.type === "radio") node.checked = value;
  else node.value = value;
  node.listeners[event]({target: node});
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return {promise, resolve, reject};
}
const flush = () => new Promise((resolve) => setImmediate(resolve));
const clone = (value) => JSON.parse(JSON.stringify(value));
async function browser(handler) {
  const nodes = {
    "#app": new Element("main"),
    "#nav": new Element("nav"),
    "#notice": new Element("aside"),
  };
  const calls = [];
  const context = vm.createContext({
    Node: Element,
    document: {
      querySelector: (selector) => nodes[selector],
      querySelectorAll: () => [],
      createElement: (tag) => new Element(tag),
      createTextNode: (value) =>
        Object.assign(new Element("#text"), {textContent: String(value)}),
    },
    window: {addEventListener() {}, scrollTo() {}, confirm: () => true},
    location: {pathname: "/login", search: "", origin: "http://localhost"},
    history: {
      pushState(_state, _unused, url) {
        context.location.pathname = url;
      },
    },
    navigator: {},
    URLSearchParams,
    setTimeout() {},
    clearTimeout() {},
    async fetch(url, options) {
      const request = {
        url,
        method: options.method,
        body: options.body === undefined ? undefined : JSON.parse(options.body),
      };
      calls.push(request);
      const query = url.includes("?")
        ? new URLSearchParams(url.split("?")[1])
        : null;
      const data =
        url === "/api/me"
          ? {username: "owner"}
          : url.endsWith("/stats?summaryOnly=true")
            ? statisticsSummary()
            : url.includes("/stats?questionId=") && query.get("offset") === "0"
              ? statisticsPage(query.get("questionId"))
              : await handler(request);
      return {ok: true, json: async () => clone(data)};
    },
  });
  vm.runInContext(
    fs.readFileSync(
      path.join(__dirname, "../../internal/web/static/app.js"),
      "utf8",
    ),
    context,
  );
  await flush();
  return {
    calls,
    nodes,
    context,
    view: (name) => vm.runInContext(`${name}('s')`, context),
  };
}
function draft() {
  return {
    id: "s",
    revision: 1,
    title: "Original title",
    description: "Description",
    status: "draft",
    allowAnonymous: true,
    allowRepeat: false,
    deadline: null,
    questions: [
      {
        id: "q",
        title: "Question",
        type: "multiple",
        required: true,
        options: [
          {id: "one", label: "One"},
          {id: "two", label: "Two"},
        ],
        validation: {minSelect: 1},
        rules: [{operator: "contains", value: "one", target: "END"}],
      },
    ],
  };
}
function titleField(root) {
  return walk(root).find(
    (node) => node.tag === "input" && node.type === "text",
  );
}
function assertEditorLocked(root) {
  const controls = walk(root).filter((node) =>
    ["input", "textarea", "select", "button"].includes(node.tag),
  );
  assert.ok(
    controls.length > 10,
    "settings, questions, options, rules and actions are covered",
  );
  for (const control of controls)
    assert.equal(
      disabled(control),
      true,
      `${control.tag} ${text(control) || control.value} must be disabled`,
    );
}

for (const id of ["__proto__", "constructor", "toString"]) {
  for (const type of ["text", "number", "single", "multiple"]) {
    test(`${type} answers survive forward/back navigation and JSON submission with ID ${id}`, async () => {
      const value = {
        text: "A real answer",
        number: 42,
        single: "one",
        multiple: ["one"],
      }[type];
      const survey = {
        ...draft(),
        status: "published",
        questions: [
          {
            id,
            title: "Special question",
            type,
            required: true,
            options: [
              {id: "one", label: "One"},
              {id: "two", label: "Two"},
            ],
            validation: {},
          },
        ],
      };
      const ui = await browser(({url, body}) => {
        if (url.endsWith("/next"))
          return {answer: body.answer, nextQuestionId: "END"};
        if (url.endsWith("/responses")) return {};
        return survey;
      });
      const root = await ui.view("fillView");
      const control = walk(root).find(
        (node) => node.tag === (type === "text" ? "textarea" : "input"),
      );
      if (type === "text" || type === "number") {
        assert.equal(
          control.value,
          "",
          "prototype members must not appear as initial answers",
        );
        change(control, String(value));
      } else {
        assert.equal(control.checked, false);
        change(control, true, "change");
      }
      await click(findButton(root, "下一步"));
      assert.deepEqual(
        ui.calls.find((call) => call.url.endsWith("/next")).body.answer,
        value,
      );
      await click(findButton(root, "上一步"));
      const restored = walk(root).find(
        (node) => node.tag === (type === "text" ? "textarea" : "input"),
      );
      if (type === "text" || type === "number")
        assert.equal(restored.value, String(value));
      else assert.equal(restored.checked, true);
      await click(findButton(root, "下一步"));
      await click(findButton(root, "提交问卷"));
      const answers = ui.calls.find((call) => call.url.endsWith("/responses"))
        .body.answers;
      assert.deepEqual(Object.keys(answers), [id]);
      assert.deepEqual(answers[id], value);
      assert.match(text(root), /提交成功/);
    });
  }
}

for (const result of ["resolve", "reject"]) {
  test(`submitting blocks back navigation until the response ${result}s`, async () => {
    const pending = deferred();
    const survey = {
      ...draft(),
      status: "published",
      questions: [
        {
          id: "q",
          title: "Question",
          type: "text",
          required: true,
          validation: {},
        },
      ],
    };
    const ui = await browser(({url, body}) => {
      if (url.endsWith("/next"))
        return {answer: body.answer, nextQuestionId: "END"};
      if (url.endsWith("/responses")) return pending.promise;
      return survey;
    });
    const root = await ui.view("fillView");
    change(
      walk(root).find((n) => n.tag === "textarea"),
      "My answer",
    );
    await click(findButton(root, "下一步"));
    const back = findButton(root, "上一步");
    const submit = click(findButton(root, "提交问卷"));
    assert.equal(
      disabled(back),
      true,
      "Back must be disabled while writing a response",
    );
    const anonymous = walk(root).find(
      (n) => n.tag === "input" && n.type === "checkbox",
    );
    assert.equal(
      disabled(anonymous),
      true,
      "Anonymity must not be changed during submission",
    );
    await click(back);
    assert.match(
      text(root),
      /已完成填写/,
      "must stay on final screen while request is pending",
    );
    if (result === "resolve") {
      pending.resolve({});
      await submit;
      assert.match(text(root), /提交成功/);
      assert.equal(
        ui.calls.filter((c) => c.url.endsWith("/responses")).length,
        1,
      );
    } else {
      pending.reject(new Error("Submit unavailable"));
      await submit;
      assert.equal(
        disabled(back),
        false,
        "Back is available again after a failed submit",
      );
      assert.match(text(ui.nodes["#notice"]), /Submit unavailable/);
      await click(back);
      assert.match(text(root), /Question/);
    }
  });
}

test("saving locks every editor control, preserves entered content and blocks concurrent requests", async () => {
  const pending = deferred();
  const ui = await browser((request) =>
    request.method === "PUT" ? pending.promise : draft(),
  );
  const root = await ui.view("editorView");
  change(titleField(root), "My edited title");
  const saving = click(findButton(root, "保存草稿"));
  assertEditorLocked(root);
  change(titleField(root), "Attempted input during save");
  await click(findButton(root, "保存并发布"));
  await click(findButton(root, "＋ 添加题目"));
  assert.equal(ui.calls.filter((call) => call.method === "PUT").length, 1);
  assert.equal(
    ui.calls.filter((call) => call.url.endsWith("/publish")).length,
    0,
  );
  pending.resolve({
    ...ui.calls.find((call) => call.method === "PUT").body,
    revision: 2,
  });
  await saving;
  assert.equal(titleField(root).value, "My edited title");
  assert.equal(disabled(titleField(root)), false);
  assert.match(text(ui.nodes["#notice"]), /草稿已保存/);
});

for (const failure of ["save", "publish"]) {
  test(`${failure} failure unlocks the editor and retains changes for retry`, async () => {
    const pending = deferred();
    let fail = true;
    const ui = await browser((request) => {
      if (request.method === "PUT")
        return failure === "save" && fail
          ? pending.promise
          : {...request.body, revision: 2};
      if (request.url.endsWith("/publish"))
        return fail ? pending.promise : {...draft(), status: "published"};
      return draft();
    });
    const root = await ui.view("editorView");
    change(titleField(root), "Keep this edit");
    const saving = click(
      findButton(root, failure === "save" ? "保存草稿" : "保存并发布"),
    );
    await flush();
    assertEditorLocked(root);
    pending.reject(new Error("Temporary failure"));
    await saving;
    assert.equal(titleField(root).value, "Keep this edit");
    assert.equal(disabled(titleField(root)), false);
    assert.match(text(ui.nodes["#notice"]), /Temporary failure/);
    fail = false;
    await click(findButton(root, "保存草稿"));
    assert.equal(
      ui.calls.filter((call) => call.method === "PUT").at(-1).body.title,
      "Keep this edit",
    );
    assert.match(text(ui.nodes["#notice"]), /草稿已保存/);
  });
}

test("publishing keeps the entire editor locked between saving and publishing", async () => {
  const saved = deferred(),
    published = deferred();
  const ui = await browser((request) => {
    if (request.method === "PUT") return saved.promise;
    if (request.url.endsWith("/publish")) return published.promise;
    return draft();
  });
  const root = await ui.view("editorView");
  const publishing = click(findButton(root, "保存并发布"));
  assertEditorLocked(root);
  saved.resolve({...draft(), revision: 2});
  await flush();
  assertEditorLocked(root);
  await click(findButton(root, "保存草稿"));
  assert.equal(ui.calls.filter((call) => call.method === "PUT").length, 1);
  published.resolve({...draft(), status: "published", revision: 3});
  await publishing;
  assert.match(text(root), /已发布/);
  assert.equal(findButton(root, "保存草稿"), undefined);
});

function statistics() {
  return {
    totalResponses: 21,
    questions: [
      {
        id: "text & special",
        title: "Text answers",
        type: "text",
        answered: 21,
        values: Array.from({length: 20}, (_, i) => `Answer ${i + 1}`),
        valuesOffset: 0,
        valuesLimit: 20,
        hasMore: true,
      },
      {
        id: "number",
        title: "Number answers",
        type: "number",
        answered: 21,
        average: 11,
        values: Array.from({length: 20}, (_, i) => i + 1),
        valuesOffset: 0,
        valuesLimit: 20,
        hasMore: true,
      },
      {
        id: "choice",
        title: "Choice answers",
        type: "single",
        answered: 21,
        options: [{id: "one", label: "One", count: 21}],
      },
    ],
  };
}
function statisticsSummary() {
  const data = statistics();
  for (const q of data.questions) {
    q.values = [];
    q.valuesOffset = 0;
    q.valuesLimit = 0;
    q.hasMore = q.answered > 0;
  }
  return data;
}
function statisticsPage(questionID) {
  const q = statistics().questions.find((q) => q.id === questionID);
  assert.ok(q, "known paginated question");
  return {totalResponses: 21, questions: [q]};
}
function card(root, title) {
  return walk(root).find(
    (node) =>
      node.tag === "section" &&
      node.children.some((child) => text(child).includes(title)) &&
      walk(node).some((child) => child.tag === "h2" && text(child) === title),
  );
}

test("statistics page raw text and numbers independently, keeping complete totals, choices and averages", async () => {
  const pending = deferred();
  let waitForPage = true;
  const ui = await browser((request) => {
    if (request.url.includes("/stats?")) {
      const params = new URLSearchParams(request.url.split("?")[1]);
      if (waitForPage && params.get("offset") === "20") return pending.promise;
      const question = statistics().questions.find(
        (q) => q.id === params.get("questionId"),
      );
      return {
        totalResponses: 21,
        questions: [
          {
            ...question,
            valuesOffset: Number(params.get("offset")),
            values: params.get("offset") === "0" ? question.values : [21],
            hasMore: params.get("offset") === "0",
          },
        ],
      };
    }
    return request.url.endsWith("/stats") ? statistics() : draft();
  });
  const root = await ui.view("statsView");
  const textCard = card(root, "Text answers"),
    numberCard = card(root, "Number answers"),
    choiceCard = card(root, "Choice answers");
  assert.equal(
    findButton(textCard, "下一页"),
    undefined,
    "raw answers are not eagerly loaded",
  );
  await click(findButton(textCard, "查看填写内容"));
  await click(findButton(numberCard, "查看填写内容"));
  assert.ok(disabled(findButton(textCard, "上一页")));
  const page = click(findButton(textCard, "下一页"));
  assert.ok(disabled(findButton(textCard, "下一页")));
  assert.ok(disabled(findButton(textCard, "上一页")));
  await click(findButton(textCard, "下一页"));
  assert.equal(
    ui.calls.filter((call) => call.url.includes("offset=20")).length,
    1,
  );
  const params = new URLSearchParams(ui.calls.at(-1).url.split("?")[1]);
  assert.equal(params.get("questionId"), "text & special");
  assert.equal(params.get("offset"), "20");
  assert.equal(params.get("limit"), "20");
  pending.resolve({
    totalResponses: 21,
    questions: [
      {
        ...statistics().questions[0],
        values: ["Final answer"],
        valuesOffset: 20,
        hasMore: false,
      },
    ],
  });
  await page;
  assert.match(text(textCard), /Final answer/);
  assert.doesNotMatch(text(textCard), /Answer 1/);
  assert.equal(walk(textCard).find((node) => node.tag === "ol").start, 21);
  assert.ok(disabled(findButton(textCard, "下一页")));
  assert.equal(
    card(root, "Number answers"),
    numberCard,
    "other questions retain their DOM and page",
  );
  assert.equal(card(root, "Choice answers"), choiceCard);
  assert.match(text(numberCard), /平均值：11/);
  assert.match(text(choiceCard), /21 次选择 · 100%/);
  waitForPage = false;
  await click(findButton(textCard, "上一页"));
  assert.match(text(textCard), /Answer 1/);
  await click(findButton(numberCard, "下一页"));
  assert.match(text(numberCard), /平均值：11/);
  assert.match(text(numberCard), /21 份回答/);
  assert.deepEqual(
    walk(numberCard)
      .filter((node) => node.tag === "li")
      .map(text),
    ["21"],
  );
});

test("statistics pagination refreshes returned summaries when new submissions arrive", async () => {
  const pending = deferred();
  const ui = await browser((request) => {
    if (request.url.includes("/stats?")) return pending.promise;
    return request.url.endsWith("/stats") ? statistics() : draft();
  });
  const root = await ui.view("statsView");
  const numberCard = card(root, "Number answers"),
    choiceCard = card(root, "Choice answers");
  const total = walk(root).find((node) => node.className === "large-number");
  const originalChoices = text(choiceCard);
  await click(findButton(numberCard, "查看填写内容"));
  const paging = click(findButton(numberCard, "下一页"));
  assert.equal(
    text(total),
    "21",
    "keep the previous total while the request is pending",
  );
  assert.match(text(numberCard), /平均值：11/);
  pending.resolve({
    totalResponses: 22,
    questions: [
      {
        ...statistics().questions[1],
        answered: 22,
        average: 15,
        values: [21, 99],
        valuesOffset: 20,
        hasMore: false,
      },
    ],
  });
  await paging;
  assert.match(text(numberCard), /平均值：15/);
  assert.match(text(numberCard), /22 份回答/);
  assert.equal(text(total), "22");
  assert.deepEqual(
    walk(numberCard)
      .filter((node) => node.tag === "li")
      .map(text),
    ["21", "99"],
  );
  assert.equal(
    text(choiceCard),
    originalChoices,
    "do not fabricate refreshed summaries for other questions",
  );
  assert.match(text(card(root, "Text answers")), /21 份回答/);
});

test("failed statistics pagination retains the current page and allows retry", async () => {
  let fail = true;
  const ui = await browser((request) => {
    if (request.url.includes("/stats?")) {
      if (fail) throw new Error("Page unavailable");
      return {
        totalResponses: 21,
        questions: [
          {
            ...statistics().questions[0],
            values: ["Last"],
            valuesOffset: 20,
            hasMore: false,
          },
        ],
      };
    }
    return request.url.endsWith("/stats") ? statistics() : draft();
  });
  const root = await ui.view("statsView");
  const question = card(root, "Text answers");
  await click(findButton(question, "查看填写内容"));
  await click(findButton(question, "下一页"));
  assert.match(text(question), /Answer 1/);
  assert.equal(
    text(walk(root).find((node) => node.className === "large-number")),
    "21",
  );
  assert.match(text(ui.nodes["#notice"]), /Page unavailable/);
  assert.equal(disabled(findButton(question, "下一页")), false);
  fail = false;
  await click(findButton(question, "下一页"));
  assert.match(text(question), /Last/);
});

test("switching question filters during pagination refreshes the visible card and releases its controls", async () => {
  const pending = deferred();
  const ui = await browser((request) => {
    if (request.url.includes("/stats?")) return pending.promise;
    return request.url.endsWith("/stats") ? statistics() : draft();
  });
  const root = await ui.view("statsView");
  await click(findButton(card(root, "Text answers"), "查看填写内容"));
  const paging = click(findButton(card(root, "Text answers"), "下一页"));
  const filter = walk(root).find((node) => node.tag === "select");
  change(filter, "number", "change");
  assert.equal(card(root, "Text answers"), undefined);
  change(filter, "text & special", "change");
  const visible = card(root, "Text answers");
  assert.ok(disabled(findButton(visible, "下一页")));
  pending.resolve({
    totalResponses: 21,
    questions: [
      {
        ...statistics().questions[0],
        values: ["Last"],
        valuesOffset: 20,
        hasMore: false,
      },
    ],
  });
  await paging;
  assert.match(text(visible), /Last/);
  assert.equal(disabled(findButton(visible, "上一页")), false);
});

for (const outcome of ["resolve", "reject"]) {
  test(`statistics ignores a stale ${outcome} after navigation`, async () => {
    const pending = deferred();
    const ui = await browser((request) => {
      if (request.url.includes("/stats?")) return pending.promise;
      return request.url.endsWith("/stats") ? statistics() : draft();
    });
    const root = await ui.view("statsView");
    ui.nodes["#app"].replaceChildren(root);
    await click(findButton(card(root, "Text answers"), "查看填写内容"));
    const paging = click(findButton(card(root, "Text answers"), "下一页"));
    vm.runInContext("go('/login')", ui.context);
    await flush();
    const current = text(ui.nodes["#app"]);
    if (outcome === "resolve")
      pending.resolve({
        totalResponses: 22,
        questions: [
          {
            ...statistics().questions[0],
            values: ["Stale value"],
            valuesOffset: 20,
            hasMore: false,
          },
        ],
      });
    else pending.reject(new Error("Stale error"));
    await paging;
    assert.equal(text(ui.nodes["#app"]), current);
    assert.equal(ui.nodes["#notice"].hidden, true);
    assert.doesNotMatch(text(root), /Stale value/);
    assert.equal(
      text(walk(root).find((node) => node.className === "large-number")),
      "21",
    );
  });
}

function ruleEditorField(root, label, tag) {
  const container = walk(root).find(
    (node) =>
      node.tag === "label" &&
      node.children[0]?.tag === "span" &&
      text(node.children[0]) === label &&
      node.children.some((child) => child.tag === tag),
  );
  assert.ok(container, `rule field ${label} exists`);
  return container.children.find((child) => child.tag === tag);
}

test("single-choice branching uses visible option labels while storing stable IDs", async () => {
  const survey = {
    ...draft(),
    questions: [
      {
        id: "q1",
        title: "男或女",
        type: "single",
        required: true,
        options: [
          {id: "male_id", label: "男"},
          {id: "female_id", label: "女"},
        ],
        validation: {},
        rules: [],
      },
      {
        id: "q2",
        title: "默认下一题",
        type: "text",
        required: true,
        validation: {},
        rules: [],
      },
      {
        id: "q3",
        title: "女生题目",
        type: "text",
        required: true,
        validation: {},
        rules: [],
      },
    ],
  };
  const ui = await browser((request) => {
    if (request.method === "PUT") return {...request.body, revision: 2};
    return survey;
  });
  const root = await ui.view("editorView");
  await click(findButton(root, "＋ 增加规则"));
  const value = ruleEditorField(root, "条件值", "select");
  assert.equal(
    value["aria-label"],
    "条件值",
    "Select must have an explicit accessible name",
  );
  assert.equal(
    walk(value).some((node) => node.tag === "option" && text(node) === "女"),
    true,
  );
  change(value, "female_id", "change");
  assert.equal(
    ruleEditorField(root, "跳转到", "select")["aria-label"],
    "跳转到",
  );
  change(ruleEditorField(root, "跳转到", "select"), "q3", "change");
  await click(findButton(root, "保存草稿"));
  const saved = ui.calls.find((c) => c.method === "PUT").body;
  assert.equal(saved.questions[0].rules[0].operator, "eq");
  assert.equal(saved.questions[0].rules[0].value, "female_id");
  assert.equal(saved.questions[0].rules[0].target, "q3");
});

test("multiple-choice contains_all selects labels but persists an array of IDs", async () => {
  const survey = {
    ...draft(),
    questions: [
      {
        id: "q1",
        title: "喜欢的水果",
        type: "multiple",
        required: true,
        options: [
          {id: "apple", label: "苹果"},
          {id: "pear", label: "梨"},
        ],
        validation: {},
        rules: [],
      },
      {
        id: "q2",
        title: "下一题",
        type: "text",
        required: true,
        validation: {},
        rules: [],
      },
    ],
  };
  const ui = await browser((request) =>
    request.method === "PUT" ? {...request.body, revision: 2} : survey,
  );
  const root = await ui.view("editorView");
  await click(findButton(root, "＋ 增加规则"));
  change(ruleEditorField(root, "如果答案", "select"), "contains_all", "change");
  const pear = walk(root).find(
    (node) =>
      node.tag === "label" &&
      node.className === "check" &&
      node.children.some(
        (child) => child.tag === "input" && child.type === "checkbox",
      ) &&
      text(node).includes("梨"),
  );
  assert.ok(pear);
  change(pear.children[0], true, "change");
  await click(findButton(root, "保存草稿"));
  assert.deepEqual(
    ui.calls.find((c) => c.method === "PUT").body.questions[0].rules[0].value,
    ["apple", "pear"],
  );
});

test("dashboard hides archived questionnaires until requested", async () => {
  const original = {...draft(), id: "s", title: "Visible draft"};
  const archive = {
    ...draft(),
    id: "archive",
    title: "Archived survey",
    status: "closed",
    archived: true,
  };
  const ui = await browser(({url}) =>
    url === "/api/surveys" ? [original, archive] : original,
  );
  const root = await ui.view("dashboard");
  assert.match(text(root), /Visible draft/);
  assert.doesNotMatch(text(root), /Archived survey/);
  await click(findButton(root, "查看已归档问卷"));
  assert.match(text(root), /Archived survey/);
  assert.doesNotMatch(text(root), /Visible draft/);
});

test("editor offers three question types and saves text/number as fill subtypes", async () => {
  const ui = await browser((req) =>
    req.method === "PUT" ? {...req.body, revision: 2} : {...draft(), questions: []},
  );
  const root = await ui.view("editorView");
  const addTypeSelect = () =>
    walk(root).find((n) =>
      n.tag === "select" && n.children.some((x) => x.tag === "option" && x.value === "fill"),
    );
  const cards = () =>
    walk(root).filter((n) => n.tag === "fieldset" && n.className === "card question");

  let addType = addTypeSelect();
  assert.deepEqual(addType.children.map((n) => n.value), ["single", "multiple", "fill"]);
  change(addType, "fill", "change");
  await click(findButton(root, "＋ 添加题目"));
  assert.match(text(cards()[0]), /填空题（文本）/);
  change(ruleEditorField(cards()[0], "题目", "input"), "意见");

  addType = addTypeSelect();
  change(addType, "fill", "change");
  await click(findButton(root, "＋ 添加题目"));
  change(ruleEditorField(cards()[1], "填空内容", "select"), "number", "change");
  change(ruleEditorField(cards()[1], "题目", "input"), "年龄");
  assert.match(text(cards()[1]), /填空题（数字）/);
  assert.ok(walk(cards()[1]).some((n) => n.tag === "summary" && text(n) === "填写限制"));

  await click(findButton(root, "保存草稿"));
  const questions = ui.calls.find((c) => c.method === "PUT").body.questions;
  assert.equal(questions.length, 2);
  assert.deepEqual(questions.map((q) => [q.type, q.fillType]), [
    ["fill", "text"], ["fill", "number"],
  ]);
  assert.equal(questions[0].title, "意见");
  assert.equal(questions[1].title, "年龄");
});

test("editing legacy draft uses the fill model without losing question IDs", async () => {
  const old = {...draft(), questions: [
    {id: "old_text", title: "意见", type: "text", required: true, validation: {minLength: 2}, rules: []},
    {id: "old_number", title: "年龄", type: "number", required: true, validation: {integer: true, min: 0}, rules: []},
  ]};
  const ui = await browser((req) =>
    req.method === "PUT" ? {...req.body, revision: 2} : old,
  );
  const root = await ui.view("editorView");
  await click(findButton(root, "保存草稿"));
  const questions = ui.calls.find((c) => c.method === "PUT").body.questions;
  assert.deepEqual(questions.map((q) => [q.id, q.type, q.fillType]), [
    ["old_text", "fill", "text"],
    ["old_number", "fill", "number"],
  ]);
  assert.equal(questions[0].validation.minLength, 2);
  assert.equal(questions[1].validation.integer, true);
});

for (const fillType of ["text", "number"]) {
  test(`new ${fillType} fill subtype renders and submits the right answer`, async () => {
    const survey = {...draft(), status: "published", questions: [
      {id: "q", title: "填写内容", type: "fill", fillType, required: true,
        validation: {integer: fillType === "number"}},
    ]};
    const ui = await browser((req) => {
      if (req.url.endsWith("/next"))
        return {answer: req.body.answer, nextQuestionId: "END"};
      if (req.url.endsWith("/responses")) return {};
      return survey;
    });
    const root = await ui.view("fillView");
    const control = walk(root).find((n) =>
      n.tag === (fillType === "text" ? "textarea" : "input"),
    );
    assert.ok(control);
    change(control, fillType === "text" ? "文字" : "21");
    await click(findButton(root, "下一步"));
    await click(findButton(root, "提交问卷"));
    const expected = fillType === "text" ? "文字" : 21;
    assert.deepEqual(
      ui.calls.find((c) => c.url.endsWith("/responses")).body.answers,
      {q: expected},
    );
  });
}


function libraryTemplate() {
  return {id: "s", ownerId: "owner", sharedUsernames: ["reader"], sharingRevision: 3,
    latestVersion: libraryVersions()[1]};
}
function libraryVersions() {
  return [1, 2].map((version) => ({id: `v${version}`, version,
    createdAt: "2026-01-01T00:00:00Z", question: {
      id: "template", title: `Version ${version}`, type: "fill", fillType: "text",
      required: true, validation: {}, options: [], rules: [],
    }}));
}
async function libraryBrowser(mutate, read = () => undefined) {
  const ui = await browser((request) => {
    if (request.method !== "GET") return mutate(request);
    const override = read(request);
    if (override !== undefined) return override;
    if (request.url.endsWith("/versions")) return libraryVersions();
    if (request.url.endsWith("/usage")) return {ownSurveyCount: 0, surveys: []};
    if (request.url.includes("/stats?")) return {surveyCount: 0, totalAnswers: 0, versions: []};
    return libraryTemplate();
  });
  vm.runInContext('user = {id: "owner", username: "owner"}', ui.context);
  ui.context.location.pathname = "/questions/s";
  await vm.runInContext("render()", ui.context);
  ui.root = ui.nodes["#app"].children[0];
  return ui;
}
function submitForm(form) {
  return form.listeners.submit({preventDefault() {}});
}
function libraryForm(root) {
  return walk(root).find((node) => node.tag === "form");
}
function sharingInput(root) {
  return walk(root).find((node) => node["aria-label"] === "共享用户名");
}
function assertLibraryLocked(root) {
  for (const control of walk(root).filter((node) =>
    ["input", "select", "button"].includes(node.tag))) {
    assert.equal(disabled(control), true, `${text(control) || control.value} is locked`);
  }
}

for (const operation of ["version", "restore", "sharing"]) {
  test(`library ${operation} request locks related actions and preserves drafts after failure`, async () => {
    const pending = deferred();
    const ui = await libraryBrowser(() => pending.promise);
    const root = ui.root;
    await click(findButton(root, "基于此版本编辑"));
    const form = libraryForm(root);
    change(titleField(form), "Keep my version draft");
    change(sharingInput(root), "reader, outsider");
    const saving = operation === "version" ? submitForm(form)
      : click(findButton(root, operation === "restore" ? "恢复此版本" : "保存共享设置"));
    assertLibraryLocked(root);
    await click(findButton(root, "取消编辑"));
    await click(findButton(root, "基于此版本编辑"));
    await click(findButton(root, "恢复此版本"));
    await click(findButton(root, "保存共享设置"));
    await submitForm(form); // A second submit event must also be ignored.
    change(titleField(form), "Should not replace the draft");
    change(sharingInput(root), "Should not change permissions");
    assert.equal(ui.calls.filter((call) => call.method !== "GET").length, 1);
    pending.reject(new Error("Temporary write failure"));
    await saving;
    assert.equal(libraryForm(root), form);
    assert.equal(titleField(form).value, "Keep my version draft");
    assert.equal(sharingInput(root).value, "reader, outsider");
    assert.equal(disabled(titleField(form)), false);
    assert.equal(disabled(sharingInput(root)), false);
    assert.match(text(ui.nodes["#notice"]), /Temporary write failure/);
  });
}

test("sharing uses the observed revision and synchronizes the successful server result", async () => {
  let pending = deferred();
  const ui = await libraryBrowser(() => pending.promise);
  const root = ui.root, names = sharingInput(root);
  change(names, " alice, alice ");
  const saving = click(findButton(root, "保存共享设置"));
  assert.equal(disabled(names), true);
  change(names, "bob");
  assert.deepEqual(ui.calls.at(-1).body, {usernames: ["alice", "alice"], sharingRevision: 3});
  pending.resolve({...libraryTemplate(), sharedUsernames: ["alice"], sharingRevision: 4});
  await saving;
  assert.equal(names.value, "alice");
  assert.equal(disabled(names), false);
  assert.match(text(ui.nodes["#notice"]), /共享设置已保存/);
  pending = deferred();
  change(names, "bob");
  const second = click(findButton(root, "保存共享设置"));
  assert.equal(ui.calls.at(-1).body.sharingRevision, 4);
  pending.reject(new Error("共享设置已被其他页面修改，请刷新后检查共享名单"));
  await second;
  assert.equal(names.value, "bob");
  assert.match(text(ui.nodes["#notice"]), /刷新/);
  assert.equal(ui.calls.filter((call) => call.method === "PUT").length, 2);
});

for (const operation of ["version", "restore", "sharing"]) {
  for (const outcome of ["resolve", "reject"]) {
    test(`obsolete ${operation} ${outcome} response cannot refresh or notify a new route`, async () => {
      const pending = deferred();
      const ui = await libraryBrowser(() => pending.promise);
      const root = ui.root;
      let saving;
      if (operation === "version") {
        await click(findButton(root, "基于此版本编辑"));
        saving = submitForm(libraryForm(root));
      } else saving = click(findButton(root, operation === "restore" ? "恢复此版本" : "保存共享设置"));
      ui.context.location.pathname = "/login";
      await vm.runInContext("render()", ui.context);
      const newPage = ui.nodes["#app"].children[0];
      vm.runInContext('show("New page notice", true)', ui.context);
      if (outcome === "resolve") pending.resolve({...libraryTemplate(), sharingRevision: 4});
      else pending.reject(new Error("Old page failure"));
      await saving;
      assert.equal(ui.nodes["#app"].children[0], newPage);
      assert.equal(text(ui.nodes["#notice"]), "New page notice");
    });
  }
}


for (const operation of ["version", "restore"]) {
  test(`${operation} refresh stays locked and cannot notify a route opened during refresh`, async () => {
    const pending = deferred(), refreshedVersions = deferred();
    let refreshing = false;
    const ui = await libraryBrowser(async () => {
      await pending.promise;
      refreshing = true;
      return {};
    }, (request) => refreshing && request.url.endsWith("/versions")
      ? refreshedVersions.promise : undefined);
    const root = ui.root;
    await click(findButton(root, "基于此版本编辑"));
    const saving = operation === "version" ? submitForm(libraryForm(root))
      : click(findButton(root, "恢复此版本"));
    pending.resolve({});
    await flush();
    assertLibraryLocked(root);
    assert.match(text(ui.nodes["#app"]), /正在加载/);
    ui.context.location.pathname = "/login";
    await vm.runInContext("render()", ui.context);
    const newPage = ui.nodes["#app"].children[0];
    vm.runInContext('show("New page notice", true)', ui.context);
    refreshedVersions.resolve(libraryVersions());
    await saving;
    assert.equal(ui.nodes["#app"].children[0], newPage);
    assert.equal(text(ui.nodes["#notice"]), "New page notice");
  });
}

function libraryField(form, label) {
  const field = walk(form).find((node) => node.tag === "label"
    && node.children[0] && text(node.children[0]) === label);
  assert.ok(field, `${label} field exists`);
  return field.children[1];
}
function libraryWrites(ui) {
  return ui.calls.filter((request) => request.method !== "GET");
}
function recordConfirmations(ui, accepted = false) {
  const messages = [];
  ui.context.window.confirm = (message) => { messages.push(message); return accepted; };
  return messages;
}

for (const dirtyField of ["title", "note", "validation", "options"]) {
  test(`canceling restore preserves the unsaved ${dirtyField} and sharing draft without a write`, async () => {
    const ui = await libraryBrowser(() => ({}), (request) => {
      if (dirtyField !== "options" || !request.url.endsWith("/versions")) return undefined;
      return libraryVersions().map((version) => ({...version,
        question: {...version.question, type: "single", fillType: undefined,
          options: [{id: "one", label: "One"}, {id: "two", label: "Two"}]},
      }));
    });
    const root = ui.root;
    await click(findButton(root, "基于此版本编辑"));
    const form = libraryForm(root);
    let changed;
    if (dirtyField === "title") {
      changed = titleField(form); change(changed, "Unsaved title");
    } else if (dirtyField === "note") {
      changed = libraryField(form, "版本说明"); change(changed, "Unsaved explanation");
    } else if (dirtyField === "validation") {
      changed = libraryField(form, "最少字符数"); change(changed, "7");
    } else {
      changed = walk(form).find((node) => node["aria-label"] === "选项文字");
      change(changed, "Unsaved option");
    }
    // The version form alone must trigger protection, even with unchanged sharing.
    const messages = recordConfirmations(ui);
    const expected = changed.value;
    await click(findButton(root, "恢复此版本"));
    assert.equal(messages.length, 1);
    assert.equal(libraryWrites(ui).length, 0);
    assert.equal(ui.nodes["#app"].children[0], root);
    assert.equal(libraryForm(root), form);
    assert.equal(form.parent.hidden, false);
    assert.equal(changed.value, expected);
    assert.equal(disabled(changed), false);
    // With both drafts present, cancellation must preserve both as well.
    change(sharingInput(root), "reader, outsider");
    await click(findButton(root, "恢复此版本"));
    assert.equal(messages.length, 2);
    assert.equal(libraryWrites(ui).length, 0);
    assert.equal(changed.value, expected);
    assert.equal(sharingInput(root).value, "reader, outsider");
  });
}

test("accepting restore after editing creates a restored version only after confirmation", async () => {
  const ui = await libraryBrowser(() => ({}));
  const root = ui.root;
  await click(findButton(root, "基于此版本编辑"));
  change(titleField(libraryForm(root)), "Discard this draft");
  let confirmations = 0;
  ui.context.window.confirm = () => {
    confirmations++;
    assert.equal(libraryWrites(ui).length, 0, "confirmation precedes the write");
    return true;
  };
  await click(findButton(root, "恢复此版本"));
  assert.equal(confirmations, 1);
  assert.equal(libraryWrites(ui).length, 1);
  assert.equal(libraryWrites(ui)[0].url, "/api/questions/s/restore");
  assert.equal(libraryWrites(ui)[0].body.versionId, "v2");
  assert.notEqual(ui.nodes["#app"].children[0], root);
  assert.match(text(ui.nodes["#notice"]), /已将历史内容恢复为新版本/);
});

test("an unsaved sharing list blocks restore but not opening or canceling a clean version editor", async () => {
  const ui = await libraryBrowser(() => ({}));
  const root = ui.root, messages = recordConfirmations(ui);
  change(sharingInput(root), "reader, outsider");
  await click(findButton(root, "基于此版本编辑"));
  const form = libraryForm(root);
  const editor = form.parent;
  await click(findButton(root, "取消编辑"));
  assert.equal(editor.hidden, true);
  assert.equal(messages.length, 0);
  await click(findButton(root, "恢复此版本"));
  assert.equal(messages.length, 1);
  assert.equal(libraryWrites(ui).length, 0);
  assert.equal(sharingInput(root).value, "reader, outsider");
});

test("switching version or canceling editing asks before discarding an unsaved editor", async () => {
  for (const action of ["switch", "cancel"]) {
    const ui = await libraryBrowser(() => ({}));
    const root = ui.root;
    await click(findButton(root, "基于此版本编辑"));
    const form = libraryForm(root);
    const editor = form.parent;
    change(titleField(form), "Keep editing this title");
    change(libraryField(form, "版本说明"), "Keep this note");
    change(sharingInput(root), "reader, outsider");
    const target = action === "switch"
      ? walk(root).filter((node) => node.tag === "button" && text(node) === "基于此版本编辑")[1]
      : findButton(root, "取消编辑");
    const rejected = recordConfirmations(ui);
    await click(target);
    assert.equal(rejected.length, 1);
    assert.equal(libraryForm(root), form);
    assert.equal(form.parent.hidden, false);
    assert.equal(titleField(form).value, "Keep editing this title");
    assert.equal(libraryField(form, "版本说明").value, "Keep this note");
    const accepted = recordConfirmations(ui, true);
    await click(target);
    assert.equal(accepted.length, 1);
    if (action === "switch") assert.equal(titleField(libraryForm(root)).value, "Version 1");
    else {
      assert.equal(editor.hidden, true);
      await click(findButton(root, "基于此版本编辑"));
      assert.equal(titleField(libraryForm(root)).value, "Version 2");
      assert.equal(libraryField(libraryForm(root), "版本说明").value, "");
      assert.equal(accepted.length, 1, "discarded editor no longer causes another confirmation");
    }
    assert.equal(sharingInput(root).value, "reader, outsider");
    assert.equal(libraryWrites(ui).length, 0);
  }
});

test("saving a new version confirms loss of an unsaved sharing list and retains both drafts when canceled", async () => {
  const ui = await libraryBrowser(() => ({}));
  const root = ui.root;
  await click(findButton(root, "基于此版本编辑"));
  const form = libraryForm(root);
  change(titleField(form), "New version title");
  change(libraryField(form, "版本说明"), "New version note");
  change(sharingInput(root), "reader, outsider");
  const rejected = recordConfirmations(ui);
  await submitForm(form);
  assert.equal(rejected.length, 1);
  assert.equal(libraryWrites(ui).length, 0);
  assert.equal(titleField(form).value, "New version title");
  assert.equal(libraryField(form, "版本说明").value, "New version note");
  assert.equal(sharingInput(root).value, "reader, outsider");
  assert.equal(disabled(titleField(form)), false);
  const accepted = recordConfirmations(ui, true);
  await submitForm(form);
  assert.equal(accepted.length, 1);
  assert.equal(libraryWrites(ui).length, 1);
  const request = libraryWrites(ui)[0];
  assert.equal(request.url, "/api/questions/s/versions");
  assert.equal(request.body.question.title, "New version title");
  assert.equal(request.body.note, "New version note");
  assert.equal(request.body.baseVersionId, "v2");
});

test("clean forms and changes reverted to their original values do not prompt on restore", async () => {
  for (const scenario of ["clean", "revert", "reordered constraints"]) {
    const ui = await libraryBrowser(() => ({}), (request) => {
      if (scenario !== "reordered constraints" || !request.url.endsWith("/versions")) return undefined;
      return libraryVersions().map((version) => ({...version,
        question: {...version.question, validation: {minLength: 2, maxLength: 20}},
      }));
    });
    const root = ui.root;
    await click(findButton(root, "基于此版本编辑"));
    const form = libraryForm(root);
    if (scenario === "revert") {
      change(titleField(form), "Temporary title");
      change(titleField(form), "Version 2");
      change(libraryField(form, "版本说明"), "Temporary note");
      change(libraryField(form, "版本说明"), "");
      change(libraryField(form, "最少字符数"), "7");
      change(libraryField(form, "最少字符数"), "");
      change(sharingInput(root), "outsider");
      change(sharingInput(root), "reader");
    } else if (scenario === "reordered constraints") {
      change(libraryField(form, "最少字符数"), "");
      change(libraryField(form, "最多字符数"), "");
      change(libraryField(form, "最多字符数"), "20");
      change(libraryField(form, "最少字符数"), "2");
    }
    const messages = recordConfirmations(ui);
    await click(findButton(root, "恢复此版本"));
    assert.equal(messages.length, 0);
    assert.equal(libraryWrites(ui).length, 1);
    assert.equal(libraryWrites(ui)[0].url, "/api/questions/s/restore");
  }
});

test("saving sharing retains the version draft and updates the sharing baseline for the next version save", async () => {
  const ui = await libraryBrowser((request) => request.url.endsWith("/sharing")
    ? {...libraryTemplate(), sharedUsernames: ["alice"], sharingRevision: 4} : ({}));
  const root = ui.root;
  await click(findButton(root, "基于此版本编辑"));
  const form = libraryForm(root);
  change(titleField(form), "Still editing my version");
  change(sharingInput(root), "alice");
  const messages = recordConfirmations(ui);
  await click(findButton(root, "保存共享设置"));
  assert.equal(messages.length, 0);
  assert.equal(libraryForm(root), form);
  assert.equal(titleField(form).value, "Still editing my version");
  assert.equal(sharingInput(root).value, "alice");
  await submitForm(form);
  assert.equal(messages.length, 0, "saving its own draft does not require discard confirmation");
  assert.deepEqual(libraryWrites(ui).map((request) => request.url), [
    "/api/questions/s/sharing", "/api/questions/s/versions",
  ]);
});

test("reverting fill types ignores API default values but still protects removed constraints", async () => {
  for (const fillType of ["text", "number"]) {
    for (const [constrained, switchQuestionType] of [[false, false], [true, false], [false, true], [true, true]]) {
      const ui = await libraryBrowser(() => ({}), (request) => {
        if (!request.url.endsWith("/versions")) return undefined;
        const validation = {integer: false};
        if (constrained) Object.assign(validation, fillType === "text"
          ? {minLength: 2, maxLength: 20} : {min: 2, max: 20});
        return libraryVersions().map((version) => ({...version,
          question: {...version.question, fillType, options: null, validation},
        }));
      });
      const root = ui.root;
      await click(findButton(root, "基于此版本编辑"));
      const form = libraryForm(root);
      if (switchQuestionType) {
        change(libraryField(form, "题目类型"), "single", "change");
        change(libraryField(form, "题目类型"), "fill", "change");
      } else change(libraryField(form, "填空内容"), fillType === "text" ? "number" : "text", "change");
      change(libraryField(form, "填空内容"), fillType, "change");
      const messages = recordConfirmations(ui);
      await click(findButton(root, "恢复此版本"));
      assert.equal(messages.length, constrained ? 1 : 0,
        `${fillType}, ${switchQuestionType ? "question type" : "fill subtype"}: ${constrained ? "lost constraints need protection" : "unchanged visible settings need no confirmation"}`);
      assert.equal(libraryWrites(ui).length, constrained ? 0 : 1);
      if (constrained) {
        assert.equal(libraryForm(root), form);
        assert.equal(libraryField(form, "填空内容").value, fillType);
      }
    }
  }
});
