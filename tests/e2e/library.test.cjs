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
async function mutation(page, path, method, action, expected = 200) {
  const responsePromise = page.waitForResponse((response) =>
    new URL(response.url()).pathname === path && response.request().method() === method,
  );
  await action();
  const response = await responsePromise;
  assert.equal(response.status(), expected, await response.text());
  return response.json();
}
async function heading(page, name) {
  await page.getByRole("heading", {name, exact: true}).waitFor();
}
function historyCard(page, version) {
  return page.locator("article.card").filter({
    has: page.getByRole("heading", {name: "v" + version, exact: true}),
  });
}

// Uses the Go runner's isolated MongoDB and actual embedded server. Fixtures use
// HTTP where repetitive; all new management flows below use rendered controls.
test("real browser: immutable reusable versions, banks, sharing and owned statistics",
  {timeout: 150000}, async (t) => {
    assert.ok(baseURL, "E2E_BASE_URL must be provided by the Go test server");
    const launch = {headless: true};
    if (process.env.PLAYWRIGHT_EXECUTABLE_PATH)
      launch.executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
    const browser = await chromium.launch(launch);
    t.after(() => browser.close());
    const owner = await browser.newContext();
    const recipient = await browser.newContext();
    await request(owner, "POST", "/api/register", {username: "library_owner", password: "library-password-123"}, 201);
    await request(recipient, "POST", "/api/register", {username: "library_reader", password: "library-password-123"}, 201);
    const page = await owner.newPage(), readerPage = await recipient.newPage();
    const errors = [];
    for (const current of [page, readerPage]) {
      current.setDefaultTimeout(10000);
      current.on("pageerror", (error) => errors.push(error.message));
    }
    let template, versions, bank, first, second, third, readerSurvey;

    async function surveyFromPicker(title, versionId, bankId) {
      await page.goto(baseURL + "/");
      await page.getByRole("button", {name: "＋ 新建问卷", exact: true}).click();
      await page.waitForURL("**/edit/*");
      const id = new URL(page.url()).pathname.split("/").pop();
      await page.getByLabel("问卷标题", {exact: true}).fill(title);
      await page.getByLabel("问卷说明", {exact: true}).fill("导入题目时保留未保存说明");
      await page.getByLabel("允许重复提交", {exact: true}).check();
      await page.getByRole("button", {name: "从我的题目或题库添加", exact: true}).click();
      if (bankId) {
        await page.getByLabel("或选择题库", {exact: true}).selectOption(bankId);
        await page.getByRole("button", {name: "添加此题库版本", exact: true}).click();
      } else {
        await page.getByLabel("选择题目", {exact: true}).selectOption(template.id);
        await page.getByLabel("题目版本", {exact: true}).selectOption(versionId);
        await page.getByRole("button", {name: "添加选定版本", exact: true}).click();
      }
      assert.equal(await page.getByLabel("问卷标题", {exact: true}).inputValue(), title);
      assert.equal(await page.getByLabel("问卷说明", {exact: true}).inputValue(), "导入题目时保留未保存说明");
      assert.equal(await page.getByLabel("题目", {exact: true}).isDisabled(), true);
      assert.equal(await page.getByLabel("必填", {exact: true}).isDisabled(), true);
      // Canonical content is locked while rules belong to this survey instance.
      const card = page.locator("fieldset.question");
      await card.getByText(/条件跳转/).click();
      assert.equal(await card.getByRole("button", {name: "＋ 增加规则", exact: true}).isEnabled(), true);
      await page.getByRole("button", {name: "保存并发布", exact: true}).click();
      await heading(page, "问卷详情");
      const saved = await request(owner, "GET", "/api/surveys/" + id);
      assert.equal(saved.questions[0].sourceVersionId, versionId);
      assert.equal(saved.questions[0].sourceQuestionId, template.id);
      assert.notEqual(saved.questions[0].id, "template");
      return saved;
    }

    await t.test("create a reusable numeric question and pin it in a bank through UI", async () => {
      await page.goto(baseURL + "/questions");
      await page.getByRole("button", {name: "＋ 新建题目", exact: true}).click();
      await page.getByLabel("题目类型", {exact: true}).selectOption("fill");
      await page.getByLabel("填空内容", {exact: true}).selectOption("number");
      await page.getByLabel("题目标题", {exact: true}).fill("复用评分");
      await page.getByLabel("最小数值", {exact: true}).fill("0");
      await page.getByLabel("只接受整数", {exact: true}).check();
      await page.getByLabel("版本说明", {exact: true}).fill("初始数字题");
      template = await mutation(page, "/api/questions", "POST", () =>
        page.getByRole("button", {name: "保存题目", exact: true}).click(), 201);
      await heading(page, "版本历史");
      versions = await request(owner, "GET", "/api/questions/" + template.id + "/versions");
      assert.equal(versions.length, 1);
      assert.equal(versions[0].question.fillType, "number");
      assert.equal(versions[0].question.validation.integer, true);
      await page.getByRole("link", {name: "题库", exact: true}).click();
      await page.getByLabel("题库名称", {exact: true}).fill("固定评分题库");
      bank = await mutation(page, "/api/question-banks", "POST", () =>
        page.getByRole("button", {name: "新建题库", exact: true}).click(), 201);
      await heading(page, "管理题库");
      await page.getByRole("button", {name: "添加题目版本", exact: true}).click();
      await page.getByLabel("选择题目", {exact: true}).selectOption(template.id);
      await page.getByRole("button", {name: "添加选定版本", exact: true}).click();
      await page.getByText("固定版本 v1", {exact: true}).waitFor();
      bank = await mutation(page, "/api/question-banks/" + bank.id, "PUT", () =>
        page.getByRole("button", {name: "保存题库", exact: true}).click());
      assert.deepEqual(bank.versionIds, [versions[0].id]);
      first = await surveyFromPicker("旧版评分问卷一", versions[0].id, bank.id);
    });

    await t.test("branch from historical versions and restore without rewriting the existing survey", async () => {
      await page.goto(baseURL + "/questions/" + template.id);
      await historyCard(page, 1).getByRole("button", {name: "基于此版本编辑", exact: true}).click();
      await page.getByLabel("题目标题", {exact: true}).fill("新版评分");
      await page.getByLabel("最大数值", {exact: true}).fill("100");
      await mutation(page, "/api/questions/" + template.id + "/versions", "POST", () =>
        page.getByRole("button", {name: "保存新版本", exact: true}).click(), 201);
      await historyCard(page, 2).waitFor();
      await historyCard(page, 1).getByRole("button", {name: "基于此版本编辑", exact: true}).click();
      await page.getByLabel("题目标题", {exact: true}).fill("旧版分支评分");
      await page.getByLabel("最大数值", {exact: true}).fill("10");
      await mutation(page, "/api/questions/" + template.id + "/versions", "POST", () =>
        page.getByRole("button", {name: "保存新版本", exact: true}).click(), 201);
      await historyCard(page, 3).getByText("基于 v1", {exact: true}).waitFor();
      await mutation(page, "/api/questions/" + template.id + "/restore", "POST", () =>
        historyCard(page, 1).getByRole("button", {name: "恢复此版本", exact: true}).click(), 201);
      await historyCard(page, 4).getByText("基于 v3 · 恢复自 v1", {exact: true}).waitFor();
      versions = await request(owner, "GET", "/api/questions/" + template.id + "/versions");
      assert.equal(versions.length, 4);
      assert.equal(versions[1].parentVersionId, versions[0].id);
      assert.equal(versions[2].parentVersionId, versions[0].id);
      assert.equal(versions[3].restoredFromVersionId, versions[0].id);
      assert.deepEqual(versions[3].question, versions[0].question);
      const unchanged = await request(owner, "GET", "/api/surveys/" + first.id);
      assert.deepEqual(unchanged.questions, first.questions);
      const unchangedBank = await request(owner, "GET", "/api/question-banks/" + bank.id);
      assert.deepEqual(unchangedBank.versionIds, [versions[0].id]);
    });

    await t.test("reuse one historical version in another survey and explicitly update only a bank pin", async () => {
      second = await surveyFromPicker("旧版评分问卷二", versions[0].id);
      assert.notEqual(second.questions[0].id, first.questions[0].id);
      await page.goto(baseURL + "/banks/" + bank.id);
      await page.getByRole("button", {name: "更换固定版本", exact: true}).click();
      await page.getByLabel("替换为版本", {exact: true}).selectOption(versions[1].id);
      await page.getByRole("button", {name: "确认替换", exact: true}).click();
      await page.getByText("固定版本 v2", {exact: true}).waitFor();
      bank = await mutation(page, "/api/question-banks/" + bank.id, "PUT", () =>
        page.getByRole("button", {name: "保存题库", exact: true}).click());
      assert.deepEqual(bank.versionIds, [versions[1].id]);
      third = await surveyFromPicker("新版评分问卷", versions[1].id, bank.id);
      assert.equal(third.questions[0].title, "新版评分");
      await page.goto(baseURL + "/edit/" + first.id);
      assert.equal(await page.getByLabel("题目", {exact: true}).inputValue(), "复用评分");
      assert.deepEqual((await request(owner, "GET", "/api/surveys/" + first.id)).questions, first.questions);
    });

    await t.test("share using usernames, show read-only versions, then revoke future access", async () => {
      await page.goto(baseURL + "/questions/" + template.id);
      await page.getByLabel("共享用户名", {exact: true}).fill("library_reader");
      await mutation(page, "/api/questions/" + template.id + "/sharing", "PUT", () =>
        page.getByRole("button", {name: "保存共享设置", exact: true}).click());
      await page.reload();
      assert.equal(await page.getByLabel("共享用户名", {exact: true}).inputValue(), "library_reader");
      await readerPage.goto(baseURL + "/questions");
      await readerPage.getByRole("link", {name: "版本、共享与统计", exact: true}).click();
      await heading(readerPage, "版本历史");
      assert.equal(await readerPage.getByRole("button", {name: "基于此版本编辑", exact: true}).count(), 0);
      assert.equal(await readerPage.getByRole("button", {name: "保存共享设置", exact: true}).count(), 0);
      readerSurvey = await request(recipient, "POST", "/api/surveys", {
        title: "其他用户私有问卷", allowRepeat: true,
        questions: [{...versions[0].question, id: "reader_q", sourceQuestionId: template.id, sourceVersionId: versions[0].id}],
      }, 201);
      await request(recipient, "POST", "/api/surveys/" + readerSurvey.id + "/publish");
      await request(recipient, "POST", "/api/public/surveys/" + readerSurvey.id + "/responses", {answers: {reader_q: 9999}}, 201);
      await page.getByLabel("共享用户名", {exact: true}).fill("");
      await mutation(page, "/api/questions/" + template.id + "/sharing", "PUT", () =>
        page.getByRole("button", {name: "保存共享设置", exact: true}).click());
      await readerPage.goto(baseURL + "/questions");
      await readerPage.getByText(/暂无可用题目/).waitFor();
      const denied = await recipient.request.get(baseURL + "/api/question-versions/" + versions[0].id);
      assert.equal(denied.status(), 404);
      await readerPage.goto(baseURL + "/survey/" + readerSurvey.id);
      await heading(readerPage, "复用评分");
      await readerPage.getByLabel("复用评分", {exact: true}).fill("8888");
      await readerPage.getByRole("button", {name: "下一步", exact: true}).click();
      await readerPage.getByRole("button", {name: "提交问卷", exact: true}).click();
      await heading(readerPage, "提交成功");
    });

    await t.test("group versions separately, page numeric answers and exclude other owners' answers", async () => {
      for (let value = 1; value <= 21; value++)
        await request(owner, "POST", "/api/public/surveys/" + first.id + "/responses", {answers: {[first.questions[0].id]: value}}, 201);
      await request(owner, "POST", "/api/public/surveys/" + second.id + "/responses", {answers: {[second.questions[0].id]: 42}}, 201);
      await request(owner, "POST", "/api/public/surveys/" + third.id + "/responses", {answers: {[third.questions[0].id]: 90}}, 201);
      await page.goto(baseURL + "/questions/" + template.id);
      const stats = page.locator("section.card").filter({has: page.getByRole("heading", {name: "跨问卷统计", exact: true})});
      await stats.getByText("3 份问卷 · 23 份题目回答", {exact: true}).waitFor();
      const oldStats = stats.locator("article").filter({has: page.getByRole("heading", {name: "v1 · 复用评分", exact: true})});
      await oldStats.getByText("2 份问卷 · 22 份回答", {exact: true}).waitFor();
      assert.match(await oldStats.textContent(), /均值：12\.409/);
      await oldStats.getByRole("heading", {name: "数值分布", exact: true}).waitFor();
      const newStats = stats.locator("article").filter({has: page.getByRole("heading", {name: "v2 · 新版评分", exact: true})});
      await newStats.getByText("均值：90", {exact: true}).waitFor();
      assert.equal(await stats.locator("ol.response-values").count(), 0);
      await page.getByLabel("统计版本", {exact: true}).selectOption(versions[0].id);
      await stats.locator("ol.response-values li").nth(19).waitFor();
      assert.deepEqual(await stats.locator("ol.response-values li").allTextContents(), Array.from({length: 20}, (_, index) => String(index + 1)));
      await stats.getByRole("button", {name: "下一页答案", exact: true}).click();
      await stats.locator("ol.response-values li").filter({hasText: /^42$/}).waitFor();
      assert.deepEqual(await stats.locator("ol.response-values li").allTextContents(), ["21", "42"]);
      await stats.getByRole("button", {name: "上一页答案", exact: true}).click();
      await stats.locator("ol.response-values li").nth(19).waitFor();
      await page.getByLabel("统计版本", {exact: true}).selectOption(versions[1].id);
      await stats.locator("ol.response-values li").filter({hasText: /^90$/}).waitFor();
      assert.deepEqual(await stats.locator("ol.response-values li").allTextContents(), ["90"]);
      assert.equal(await page.getByRole("link", {name: "其他用户私有问卷", exact: true}).count(), 0);
      const usage = page.locator("section.card").filter({has: page.getByRole("heading", {name: "使用情况", exact: true})});
      assert.match(await usage.textContent(), /全部用户合计：4 份问卷，4 处引用/);
      assert.deepEqual(errors, []);
    });

    await t.test("remove and delete a bank while referenced survey snapshots stay usable", async () => {
      await page.goto(baseURL + "/banks/" + bank.id);
      await page.getByRole("button", {name: "从题库移除", exact: true}).click();
      await page.getByText("题库暂无题目，请添加固定版本。", {exact: true}).waitFor();
      bank = await mutation(page, "/api/question-banks/" + bank.id, "PUT", () =>
        page.getByRole("button", {name: "保存题库", exact: true}).click());
      assert.deepEqual(bank.versionIds, []);
      page.once("dialog", (dialog) => dialog.accept());
      await page.getByRole("button", {name: "删除题库", exact: true}).click();
      await page.waitForURL("**/banks");
      await page.getByText("尚未创建题库。", {exact: true}).waitFor();
      await page.goto(baseURL + "/edit/" + third.id);
      assert.equal(await page.getByLabel("题目", {exact: true}).inputValue(), "新版评分");
      assert.deepEqual((await request(owner, "GET", "/api/surveys/" + first.id)).questions, first.questions);
      assert.deepEqual(errors, []);
    });
  });
