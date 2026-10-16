"use strict";
const assert = require("node:assert/strict");
const {test} = require("node:test");
// Reuse an installed test tool; no browser dependency is added to the service.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const baseURL = process.env.E2E_BASE_URL;

async function register(page, name) {
  await page.getByRole("button", {name: "没有账号？注册"}).click();
  await page.getByLabel(/用户名/).fill(name);
  await page.getByLabel(/密码/).fill("e2e-password-123");
  await page.getByRole("button", {name: "注册并登录"}).click();
}
async function post(context, path, data, expected = 200) {
  const response = await context.request.post(baseURL + path, {data});
  assert.equal(response.status(), expected, await response.text());
  return response.json();
}
async function heading(page, text) {
  await page.getByRole("heading", {name: text, exact: true}).waitFor();
}

test(
  "real browser: creation, validation, branching, anonymity, paging and closure",
  {timeout: 90000},
  async (t) => {
    assert.ok(baseURL, "E2E_BASE_URL must be provided by the Go test server");
    const launch = {headless: true};
    if (process.env.PLAYWRIGHT_EXECUTABLE_PATH)
      launch.executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
    const browser = await chromium.launch(launch);
    t.after(() => browser.close());
    const owner = await browser.newContext();
    const respondent = await browser.newContext();
    const branch = await browser.newContext();
    const page = await owner.newPage();
    const answerPage = await respondent.newPage();
    const branchPage = await branch.newPage();
    const pageErrors = [];
    for (const p of [page, answerPage, branchPage]) {
      p.setDefaultTimeout(10000);
      p.on("pageerror", (error) => pageErrors.push(error.message));
    }
    let surveyID;

    await t.test(
      "register and design three types including both fill subtypes using the actual editor",
      async () => {
        await page.goto(baseURL);
        await register(page, "e2e_owner");
        await page.getByRole("button", {name: "＋ 新建问卷"}).click();
        await page.waitForURL("**/edit/*");
        surveyID = new URL(page.url()).pathname.split("/").pop();
        await page.getByLabel("问卷标题", {exact: true}).fill("端到端体验调查");
        const typeSelector = page.locator("section.card.row select");
        assert.deepEqual(await typeSelector.locator("option").allTextContents(), [
          "单选题", "多选题", "填空题",
        ]);
        for (const type of ["single", "multiple", "fill", "fill"]) {
          await typeSelector.selectOption(type);
          await page.getByRole("button", {name: "＋ 添加题目"}).click();
        }
        const cards = page.locator("fieldset.question");
        await cards
          .nth(0)
          .getByLabel("题目", {exact: true})
          .fill("是否继续填写？");
        await cards.nth(0).getByLabel("选项文字").nth(0).fill("继续填写");
        await cards.nth(0).getByLabel("选项文字").nth(1).fill("直接结束");
        await cards
          .nth(1)
          .getByLabel("题目", {exact: true})
          .fill("请选择两项功能");
        await cards.nth(1).getByLabel("选项文字").nth(0).fill("创建问卷");
        await cards.nth(1).getByLabel("选项文字").nth(1).fill("查看统计");
        await cards.nth(1).getByText("填写限制", {exact: true}).click();
        await cards.nth(1).getByLabel("至少选择", {exact: true}).fill("2");
        await cards.nth(1).getByLabel("至多选择", {exact: true}).fill("2");
        await cards.nth(2).getByLabel("题目", {exact: true}).fill("你的建议");
        await cards.nth(3).getByLabel("填空内容", {exact: true}).selectOption("number");
        await cards.nth(3).getByLabel("题目", {exact: true}).fill("你的年龄");
        await cards.nth(3).getByText("填写限制", {exact: true}).click();
        await cards.nth(3).getByLabel("最小数值", {exact: true}).fill("0");
        await cards.nth(3).getByLabel("最大数值", {exact: true}).fill("120");
        await cards.nth(3).getByLabel("只接受整数").check();
        await cards
          .nth(0)
          .getByText(/条件跳转/)
          .click();
        await cards.nth(0).getByRole("button", {name: "＋ 增加规则"}).click();
        await cards
          .nth(0)
          .getByText(/条件跳转/)
          .click();
        // Keep a precise accessible name even though the choices have text labels.
        // The rule value still persists the stable option ID, not the display text.
        const choiceCondition = cards
          .nth(0)
          .getByLabel("条件值", {exact: true});
        assert.equal(
          await choiceCondition.getAttribute("aria-label"),
          "条件值",
        );
        await choiceCondition.selectOption({label: "直接结束"});
        await page.getByRole("button", {name: "保存并发布"}).click();
        await heading(page, "问卷详情");
        assert.equal(
          await page.getByLabel("问卷分享链接").inputValue(),
          baseURL + "/survey/" + surveyID,
        );
      },
    );

    await t.test(
      "require login and validate a complete anonymous response",
      async () => {
        await answerPage.goto(baseURL + "/survey/" + surveyID);
        await answerPage.getByRole("link", {name: "登录后继续"}).click();
        await register(answerPage, "e2e_respondent");
        await heading(answerPage, "是否继续填写？");
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await answerPage.getByRole("alert").filter({hasText: /必答/}).waitFor();
        await answerPage.getByText("继续填写", {exact: true}).click();
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await heading(answerPage, "请选择两项功能");
        await answerPage.getByText("创建问卷", {exact: true}).click();
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await answerPage
          .getByRole("alert")
          .filter({hasText: /选择数量不符合要求/})
          .waitFor();
        await answerPage.getByText("查看统计", {exact: true}).click();
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await heading(answerPage, "你的建议");
        await answerPage
          .getByLabel("你的建议", {exact: true})
          .fill("保留已有数据并持续改进。");
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await heading(answerPage, "你的年龄");
        await answerPage.getByLabel("你的年龄", {exact: true}).fill("121");
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await answerPage
          .getByRole("alert")
          .filter({hasText: /数字类型、精度或范围不符合要求/})
          .waitFor();
        await answerPage.getByLabel("你的年龄", {exact: true}).fill("21");
        await answerPage.getByRole("button", {name: "下一步"}).click();
        await heading(answerPage, "已完成填写");
        await answerPage.getByLabel(/匿名提交/).check();
        await answerPage.getByRole("button", {name: "提交问卷"}).click();
        await heading(answerPage, "提交成功");
      },
    );

    await t.test(
      "forward branching skips required questions and duplicate submissions fail",
      async () => {
        await branchPage.goto(baseURL + "/survey/" + surveyID);
        await branchPage.getByRole("link", {name: "登录后继续"}).click();
        await register(branchPage, "e2e_branch");
        await heading(branchPage, "是否继续填写？");
        await branchPage.getByText("直接结束", {exact: true}).click();
        await branchPage.getByRole("button", {name: "下一步"}).click();
        await heading(branchPage, "已完成填写");
        await branchPage.getByRole("button", {name: "提交问卷"}).click();
        await heading(branchPage, "提交成功");
        await branchPage.reload();
        await heading(branchPage, "是否继续填写？");
        await branchPage.getByText("直接结束", {exact: true}).click();
        await branchPage.getByRole("button", {name: "下一步"}).click();
        await branchPage.getByRole("button", {name: "提交问卷"}).click();
        await branchPage
          .getByRole("alert")
          .filter({hasText: /不允许重复/})
          .waitFor();
        await page.goto(baseURL + "/stats/" + surveyID);
        await heading(page, "回答统计");
        assert.equal(await page.locator(".large-number").textContent(), "2");
        assert.match(await page.locator("#app").textContent(), /平均值：21/);
      },
    );

    await t.test(
      "special question IDs and stable paginated answers work in a real browser",
      async () => {
        const survey = await post(
          owner,
          "/api/surveys",
          {
            title: "分页与特殊标识验收",
            allowRepeat: true,
            questions: [
              {
                id: "__proto__",
                title: "特殊标识文本",
                type: "fill",
                fillType: "text",
                required: true,
              },
              {id: "n", title: "评分", type: "fill", fillType: "number", required: true},
            ],
          },
          201,
        );
        await post(owner, "/api/surveys/" + survey.id + "/publish");
        await page.goto(baseURL + "/survey/" + survey.id);
        await heading(page, "特殊标识文本");
        assert.equal(
          await page.getByLabel("特殊标识文本", {exact: true}).inputValue(),
          "",
        );
        await page.getByLabel("特殊标识文本", {exact: true}).fill("第1条");
        await page.getByRole("button", {name: "下一步"}).click();
        await page.getByLabel("评分", {exact: true}).fill("1");
        await page.getByRole("button", {name: "下一步"}).click();
        await page.getByRole("button", {name: "提交问卷"}).click();
        await heading(page, "提交成功");
        for (let n = 2; n <= 22; n++)
          await post(
            owner,
            "/api/public/surveys/" + survey.id + "/responses",
            {answers: {["__proto__"]: "第" + n + "条", n}},
            201,
          );
        await page.goto(baseURL + "/stats/" + survey.id);
        await heading(page, "回答统计");
        await page.setViewportSize({width: 390, height: 844});
        const textCard = page
          .locator("section.card")
          .filter({
            has: page.getByRole("heading", {name: "特殊标识文本", exact: true}),
          });
        assert.equal(
          await textCard.getByRole("button", {name: "查看填写内容"}).count(),
          1,
        );
        await textCard.getByRole("button", {name: "查看填写内容"}).click();
        // The click starts an asynchronous fetch; wait for the complete first page
        // instead of asserting before the response has rendered.
        await textCard.locator("ol li").nth(19).waitFor();
        assert.equal(await textCard.locator("ol li").count(), 20);
        await textCard.getByRole("button", {name: "下一页"}).click();
        await textCard.getByText("第21条", {exact: true}).waitFor();
        assert.equal(await textCard.locator("ol li").count(), 2);
        assert.equal(
          await textCard.getByRole("button", {name: "下一页"}).isDisabled(),
          true,
        );
        assert.match(await page.locator("#app").textContent(), /平均值：11.5/);
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
          true,
        );
        await textCard.getByRole("button", {name: "上一页"}).click();
        await textCard.getByText("第1条", {exact: true}).waitFor();
      },
    );

    await t.test(
      "the creator closes the survey and respondents see its closed state",
      async () => {
        await page.goto(baseURL + "/edit/" + surveyID);
        await page.getByRole("button", {name: "关闭问卷"}).click();
        await page.locator(".badge.closed").waitFor();
        await answerPage.goto(baseURL + "/survey/" + surveyID);
        await answerPage
          .getByText("问卷尚未开放或已结束，暂时无法填写。")
          .waitFor();
        assert.deepEqual(pageErrors, []);
      },
    );

    await t.test(
      "creator can reopen, archive and restore without losing answers",
      async () => {
        await page.goto(baseURL + "/edit/" + surveyID);
        await page.getByRole("button", {name: "重新开启问卷"}).click();
        await page.locator(".badge.published").waitFor();
        await answerPage.goto(baseURL + "/survey/" + surveyID);
        await heading(answerPage, "是否继续填写？");

        page.once("dialog", (dialog) => dialog.accept());
        await page.getByRole("button", {name: "归档问卷"}).click();
        await page.getByRole("heading", {name: "归档管理"}).waitFor();
        await page.goto(baseURL + "/");
        await page.getByRole("button", {name: "查看已归档问卷"}).click();
        await page.getByText("端到端体验调查", {exact: true}).waitFor();
        await page.getByRole("link", {name: "查看问卷"}).first().click();
        await page.getByRole("button", {name: "恢复归档"}).click();
        await page.locator(".badge.closed").waitFor();
        await page.getByRole("button", {name: "重新开启问卷"}).click();
        await page.locator(".badge.published").waitFor();

        const result = await owner.request.get(
          baseURL + "/api/surveys/" + surveyID + "/stats?summaryOnly=true",
        );
        assert.equal(result.status(), 200);
        assert.equal((await result.json()).totalResponses, 2);
        assert.deepEqual(pageErrors, []);
      },
    );
  },
);
