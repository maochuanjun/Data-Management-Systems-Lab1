"use strict";
const assert = require("node:assert/strict");
const {test} = require("node:test");
const {chromium} = require("playwright");
const baseURL = process.env.E2E_BASE_URL;

async function request(context, method, path, data, expected = 200) {
  const response = await context.request.fetch(baseURL + path, {method, data});
  assert.equal(response.status(), expected, await response.text());
  return response.json();
}
async function heading(page, title) {
  await page.getByRole("heading", {name: title, exact: true}).waitFor();
}
async function next(page) {
  await page.getByRole("button", {name: "下一步", exact: true}).click();
}
async function back(page) {
  await page.getByRole("button", {name: "上一步", exact: true}).click();
}
async function navigateWithDialog(page, action, accept, type = "confirm") {
  const pendingDialog = page.waitForEvent("dialog", {timeout: 4000});
  const pendingAction = Promise.resolve().then(action).catch((error) => {
    // A dismissed native beforeunload intentionally produces no navigation.
    // Chromium may report either ERR_ABORTED or a navigation timeout in that
    // case; the separately observed dialog and retained input are asserted.
    if (type === "beforeunload" && !accept &&
        (/ERR_ABORTED|aborted/i.test(error.message) || error.name === "TimeoutError")) return;
    throw error;
  });
  // Both the dialog wait and the navigation may fail; consume either rejection
  // even when an earlier assertion identifies the missing protection first.
  pendingAction.catch(() => {});
  const dialog = await pendingDialog;
  assert.equal(dialog.type(), type);
  const message = dialog.message();
  if (accept) await dialog.accept();
  else await dialog.dismiss();
  await pendingAction;
  return message;
}
function textQuestion(id, title, required = true) {
  return {id, title, type: "fill", fillType: "text", required};
}
function numberQuestion(id, title) {
  return {id, title, type: "fill", fillType: "number", required: true};
}
function choiceQuestion(id, title) {
  return {id, title, type: "single", required: true, options: [
    {id: "continue_id", label: "继续填写"}, {id: "finish_id", label: "直接结束"},
  ]};
}

// Every child owns its context and survey. One missing feature must not prevent
// the remaining user feedback from being independently reproduced.
test("real browser: reported filling, editing and feedback improvements",
  {timeout: 150000}, async (t) => {
    assert.ok(baseURL, "E2E_BASE_URL must be provided by the Go test server");
    const launch = {headless: true};
    if (process.env.PLAYWRIGHT_EXECUTABLE_PATH)
      launch.executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
    const browser = await chromium.launch(launch);
    t.after(() => browser.close());
    let sequence = 0;
    async function fixture(t, questions, {publish = true, allowRepeat = true} = {}) {
      const context = await browser.newContext();
      t.after(() => context.close());
      await request(context, "POST", "/api/register", {
        username: "feedback_user_" + (++sequence), password: "feedback-password-123",
      }, 201);
      const survey = await request(context, "POST", "/api/surveys", {
        title: "用户反馈验收 " + sequence, allowAnonymous: true, allowRepeat, questions,
      }, 201);
      if (publish) await request(context, "POST", "/api/surveys/" + survey.id + "/publish");
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      t.after(() => assert.deepEqual(errors, [], "no uncaught page errors"));
      return {context, page, survey};
    }

    await t.test("unfinished text survives going back before Next has been pressed", async (t) => {
      const {page, survey} = await fixture(t, [choiceQuestion("gate", "是否继续"), textQuestion("draft", "你的建议")]);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("继续填写", {exact: true}).check();
      await next(page);
      await page.getByLabel("你的建议", {exact: true}).fill("尚未点下一步的输入 <保持原文>");
      await back(page);
      await heading(page, "是否继续");
      await next(page);
      await heading(page, "你的建议");
      assert.equal(await page.getByLabel("你的建议", {exact: true}).inputValue(), "尚未点下一步的输入 <保持原文>");
    });

    await t.test("unfinished numeric zero survives going back and returning", async (t) => {
      const {page, survey} = await fixture(t, [choiceQuestion("gate", "是否继续"), numberQuestion("zero", "填写数字")]);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("继续填写", {exact: true}).check();
      await next(page);
      await page.getByLabel("填写数字", {exact: true}).fill("0");
      await back(page);
      await next(page);
      await heading(page, "填写数字");
      assert.equal(await page.getByLabel("填写数字", {exact: true}).inputValue(), "0");
      await page.getByLabel("填写数字", {exact: true}).fill("-");
      await back(page);
      await next(page);
      await heading(page, "填写数字");
      assert.equal(await page.getByLabel("填写数字", {exact: true}).inputValue(), "-");
      let invalidRequests = 0;
      const countInvalid = r => {
        if (r.method() === "POST" && new URL(r.url()).pathname === "/api/public/surveys/" + survey.id + "/next") invalidRequests++;
      };
      page.on("request", countInvalid);
      await next(page);
      await page.getByRole("alert").filter({hasText: /第\s*2\s*题.*有限数值/}).waitFor();
      page.off("request", countInvalid);
      assert.equal(invalidRequests, 0, "unfinished numeric text is validated before requesting the next question");
      await page.getByLabel("填写数字", {exact: true}).fill("0");
      await next(page);
      await heading(page, "已完成填写");
    });

    await t.test("changing an earlier branch removes abandoned answers from the submitted payload", async (t) => {
      const gate = choiceQuestion("gate", "选择填写路径");
      gate.rules = [{operator: "eq", value: "finish_id", target: "END"}];
      const {context, page, survey} = await fixture(t, [gate, textQuestion("old_text", "旧分支文本"), numberQuestion("old_number", "旧分支数字")]);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("继续填写", {exact: true}).check();
      await next(page);
      await page.getByLabel("旧分支文本", {exact: true}).fill("不应被提交的旧答案");
      await next(page);
      await page.getByLabel("旧分支数字", {exact: true}).fill("99");
      await next(page);
      await heading(page, "已完成填写");
      await back(page);
      await back(page);
      await back(page);
      await page.getByLabel("直接结束", {exact: true}).check();
      await next(page);
      await heading(page, "已完成填写");
      assert.equal(await page.getByRole("heading", {name: /旧分支/}).count(), 0);
      const submitted = page.waitForRequest(r => r.method() === "POST" && new URL(r.url()).pathname === "/api/public/surveys/" + survey.id + "/responses");
      await page.getByRole("button", {name: "提交问卷", exact: true}).click();
      assert.deepEqual((await submitted).postDataJSON().answers, {gate: "finish_id"});
      await heading(page, "提交成功");
      const stats = await request(context, "GET", "/api/surveys/" + survey.id + "/stats");
      assert.equal(stats.totalResponses, 1);
      assert.equal(stats.questions.find(q => q.id === "old_text").answered, 0);
      assert.equal(stats.questions.find(q => q.id === "old_number").answered, 0);
    });

    await t.test("an internal list link lets the author cancel or confirm discarding an unsaved title", async (t) => {
      const {context, page, survey} = await fixture(t, [textQuestion("q", "示例")], {publish: false});
      await page.goto(baseURL + "/edit/" + survey.id);
      await page.getByLabel("问卷标题", {exact: true}).fill("未保存的标题");
      const navigate = () => page.getByRole("link", {name: "← 我的问卷", exact: true}).click();
      assert.match(await navigateWithDialog(page, navigate, false), /未保存|丢弃/);
      await page.waitForURL(baseURL + "/edit/" + survey.id);
      assert.equal(await page.getByLabel("问卷标题", {exact: true}).inputValue(), "未保存的标题");
      await navigateWithDialog(page, navigate, true);
      await page.waitForURL(baseURL + "/");
      assert.equal((await request(context, "GET", "/api/surveys/" + survey.id)).title, survey.title);
    });

    await t.test("browser Back cancellation preserves the editor URL and draft before confirmed navigation", async (t) => {
      const {page, survey} = await fixture(t, [textQuestion("q", "示例")], {publish: false});
      await page.goto(baseURL + "/");
      await page.getByRole("link", {name: "编辑问卷", exact: true}).click();
      await page.waitForURL(baseURL + "/edit/" + survey.id);
      await page.getByLabel("问卷标题", {exact: true}).fill("浏览器返回时保留");
      await navigateWithDialog(page, () => page.goBack(), false);
      await page.waitForURL(baseURL + "/edit/" + survey.id);
      assert.equal(await page.getByLabel("问卷标题", {exact: true}).inputValue(), "浏览器返回时保留");
      await navigateWithDialog(page, () => page.goBack(), true);
      await page.waitForURL(baseURL + "/");
      await heading(page, "我的问卷");
    });

    await t.test("native refresh warning can preserve or discard an unsaved title", async (t) => {
      const {page, survey} = await fixture(t, [textQuestion("q", "示例")], {publish: false});
      await page.goto(baseURL + "/edit/" + survey.id);
      await page.getByLabel("问卷标题", {exact: true}).fill("刷新前未保存");
      await navigateWithDialog(page, () => page.reload({timeout: 1500}), false, "beforeunload");
      assert.equal(page.url(), baseURL + "/edit/" + survey.id);
      assert.equal(await page.getByLabel("问卷标题", {exact: true}).inputValue(), "刷新前未保存");
      await navigateWithDialog(page, () => page.reload(), true, "beforeunload");
      await page.getByLabel("问卷标题", {exact: true}).waitFor();
      assert.equal(page.url(), baseURL + "/edit/" + survey.id);
      assert.equal(await page.getByLabel("问卷标题", {exact: true}).inputValue(), survey.title);
    });

    await t.test("an existing anonymous submission blocks re-entry before any answering controls appear", async (t) => {
      const {context, page, survey} = await fixture(t, [textQuestion("q", "只填一次")], {allowRepeat: false});
      await request(context, "POST", "/api/public/surveys/" + survey.id + "/responses", {anonymous: true, answers: {q: "第一次"}}, 201);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByText("你已经提交过这份问卷，本问卷不允许重复填写", {exact: true}).waitFor();
      assert.equal(await page.getByLabel("只填一次", {exact: true}).count(), 0);
      assert.equal(await page.getByRole("button", {name: "下一步", exact: true}).count(), 0);
      // The early message supplements, rather than replaces, final write-time enforcement.
      await request(context, "POST", "/api/public/surveys/" + survey.id + "/responses", {anonymous: false, answers: {q: "再次提交"}}, 409);
    });

    await t.test("allowRepeat still permits an already submitted account to fill and submit again", async (t) => {
      const {context, page, survey} = await fixture(t, [textQuestion("q", "可重复填写")]);
      await request(context, "POST", "/api/public/surveys/" + survey.id + "/responses", {answers: {q: "第一次"}}, 201);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("可重复填写", {exact: true}).fill("第二次");
      await next(page);
      await page.getByRole("button", {name: "提交问卷", exact: true}).click();
      await heading(page, "提交成功");
      const stats = await request(context, "GET", "/api/surveys/" + survey.id + "/stats");
      assert.equal(stats.totalResponses, 2);
    });

    await t.test("duplicate option labels prevent saving and publishing with a visible question number", async (t) => {
      const {context, page, survey} = await fixture(t, [textQuestion("first", "首题"), choiceQuestion("opaque_option_question", "重复标签题")], {publish: false});
      await page.goto(baseURL + "/edit/" + survey.id);
      const card = page.locator("fieldset.question").nth(1);
      await card.getByLabel("选项文字").nth(0).fill("相同");
      await card.getByLabel("选项文字").nth(1).fill("相同");
      for (const label of ["保存草稿", "保存并发布"]) {
        const response = page.waitForResponse(r => r.request().method() === "PUT" && new URL(r.url()).pathname === "/api/surveys/" + survey.id);
        await page.getByRole("button", {name: label, exact: true}).click();
        assert.equal((await response).status(), 400);
        const notice = page.getByRole("alert");
        await notice.filter({hasText: /第\s*2\s*题.*选项.*重复/}).waitFor();
        assert.doesNotMatch(await notice.textContent(), /opaque_option_question/);
      }
      const stored = await request(context, "GET", "/api/surveys/" + survey.id);
      assert.equal(stored.status, "draft");
      assert.notEqual(stored.questions[1].options[0].label, stored.questions[1].options[1].label);
    });

    await t.test("adding a jump rule keeps its editor expanded and immediately configurable", async (t) => {
      const {page, survey} = await fixture(t, [choiceQuestion("gate", "分支题"), textQuestion("later", "后续题")], {publish: false});
      await page.goto(baseURL + "/edit/" + survey.id);
      const card = page.locator("fieldset.question").nth(0);
      await card.getByText(/条件跳转/).click();
      await card.getByRole("button", {name: "＋ 增加规则", exact: true}).click();
      const rules = card.locator("details").filter({hasText: /条件跳转/});
      assert.equal(await rules.evaluate(node => node.open), true);
      await card.getByLabel("条件值", {exact: true}).selectOption({label: "直接结束"});
      await card.getByLabel("跳转到", {exact: true}).selectOption("END");
    });

    await t.test("the submission overview uses option labels and distinguishes explicit skips and valid empty answers", async (t) => {
      const choices = choiceQuestion("choice", "选择功能");
      choices.options = [{id: "opaque_first", label: "创建问卷"}, {id: "opaque_second", label: "查看统计"}];
      const multi = {id: "multi", title: "多个用途", type: "multiple", required: true, options: [
        {id: "opaque_multi_a", label: "教学使用"}, {id: "opaque_multi_b", label: "科研使用"},
      ]};
      const {page, survey} = await fixture(t, [choices, textQuestion("skip", "选填建议", false), multi, textQuestion("empty", "可留空文本", false), numberQuestion("number", "数字零")]);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("创建问卷", {exact: true}).check();
      await next(page);
      await page.getByRole("button", {name: "跳过此题", exact: true}).click();
      await page.getByLabel("教学使用", {exact: true}).check();
      await page.getByLabel("科研使用", {exact: true}).check();
      await next(page);
      await page.getByLabel("可留空文本", {exact: true}).fill("");
      await next(page);
      await page.getByLabel("数字零", {exact: true}).fill("0");
      await next(page);
      await heading(page, "答案概览");
      const overview = page.locator("section.card").filter({has: page.getByRole("heading", {name: "答案概览", exact: true})});
      for (const label of ["创建问卷", "教学使用", "科研使用", "已跳过", "已回答（内容为空）"])
        assert.match(await overview.textContent(), new RegExp(label.replace(/[（）]/g, "\\$&")));
      assert.doesNotMatch(await overview.textContent(), /opaque_first|opaque_second|opaque_multi_/);
      const zero = overview.locator("article").filter({has: page.getByRole("heading", {name: "第 5 题：数字零", exact: true})});
      assert.match(await zero.textContent(), /0/);
      assert.equal(await overview.locator("article").count(), 5);
    });

    await t.test("answer validation identifies the visible question number instead of its internal ID", async (t) => {
      const number = numberQuestion("opaque_validation_id", "年龄范围");
      number.validation = {min: 0, max: 120, integer: true};
      const {page, survey} = await fixture(t, [choiceQuestion("first", "首题选择"), number]);
      await page.goto(baseURL + "/survey/" + survey.id);
      await page.getByLabel("继续填写", {exact: true}).check();
      await next(page);
      await page.getByLabel("年龄范围", {exact: true}).fill("121");
      await next(page);
      const notice = page.getByRole("alert");
      await notice.filter({hasText: /第\s*2\s*题.*范围/}).waitFor();
      assert.doesNotMatch(await notice.textContent(), /opaque_validation_id/);
      assert.equal(await page.getByLabel("年龄范围", {exact: true}).inputValue(), "121");
    });
  });
