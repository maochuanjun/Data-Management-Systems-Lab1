"use strict";
const assert = require("node:assert/strict");
const {test} = require("node:test");
const {chromium} = require("playwright");
const baseURL = process.env.E2E_BASE_URL;

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return {promise, resolve};
}

async function request(context, method, path, data, expected = 200) {
  const response = await context.request.fetch(baseURL + path, {method, data});
  assert.equal(response.status(), expected, await response.text());
  return response.json();
}

// Hold the actual server response so assertions run while the UI is saving.
// No timer determines ordering, and cleanup releases the request on failure.
async function holdMutation(t, page, path, method) {
  const received = deferred(), release = deferred();
  let pending;
  const handler = (route) => {
    if (route.request().method() !== method) return route.continue();
    pending = (async () => {
      const response = await route.fetch();
      received.resolve(route.request().postDataJSON());
      await release.promise;
      await route.fulfill({response});
    })();
    return pending;
  };
  await page.route(baseURL + path, handler);
  t.after(async () => {
    release.resolve();
    await pending;
    await page.unroute(baseURL + path, handler);
  });
  return {received: received.promise, release: release.resolve};
}

async function answerConfirmation(page, action, accept) {
  const dialogPromise = page.waitForEvent("dialog");
  const actionPromise = action();
  const dialog = await dialogPromise;
  if (accept) await dialog.accept();
  else await dialog.dismiss();
  await actionPromise;
  assert.equal(dialog.type(), "confirm");
  assert.match(dialog.message(), /未保存/);
}

async function withoutConfirmation(page, action) {
  const messages = [];
  const handler = (dialog) => { messages.push(dialog.message()); return dialog.dismiss(); };
  page.on("dialog", handler);
  try { await action(); }
  finally { page.off("dialog", handler); }
  assert.deepEqual(messages, [], "unchanged drafts must not require confirmation");
}

test("real browser: question mutations preserve drafts and sharing permissions",
  {timeout: 90000}, async (t) => {
    assert.ok(baseURL, "E2E_BASE_URL must be provided by the Go test server");
    const launch = {headless: true};
    if (process.env.PLAYWRIGHT_EXECUTABLE_PATH)
      launch.executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
    const browser = await chromium.launch(launch);
    t.after(() => browser.close());
    const owner = await browser.newContext(), reader = await browser.newContext();
    await request(owner, "POST", "/api/register", {username: "race_owner", password: "library-race-password"}, 201);
    await request(reader, "POST", "/api/register", {username: "race_reader", password: "library-race-password"}, 201);
    const errors = [];

    async function fixture(t) {
      const template = await request(owner, "POST", "/api/questions", {
        question: {id: "template", title: "并发操作回归", type: "fill", fillType: "text", required: true},
      }, 201);
      const page = await owner.newPage();
      page.setDefaultTimeout(10000);
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto(baseURL + "/questions/" + template.id);
      await page.getByRole("heading", {name: "版本历史", exact: true}).waitFor();
      return {page, path: "/api/questions/" + template.id};
    }

    for (const action of ["versions", "restore"]) {
      await t.test(action + " blocks competing edits and sharing until the response arrives", async (t) => {
        const {page, path} = await fixture(t);
        const held = await holdMutation(t, page, path + "/" + action, "POST");
        if (action === "versions") {
          await page.getByRole("button", {name: "基于此版本编辑", exact: true}).click();
          await page.getByLabel("题目标题", {exact: true}).fill("已提交的版本草稿");
          await page.getByRole("button", {name: "保存新版本", exact: true}).click();
        } else {
          await page.getByRole("button", {name: "恢复此版本", exact: true}).click();
        }
        await held.received;
        for (const name of ["基于此版本编辑", "恢复此版本", "保存共享设置"])
          assert.equal(await page.getByRole("button", {name, exact: true}).isDisabled(), true, name);
        assert.equal(await page.getByLabel("共享用户名", {exact: true}).isDisabled(), true);
        if (action === "versions") {
          assert.equal(await page.getByLabel("题目标题", {exact: true}).isDisabled(), true);
          assert.equal(await page.getByRole("button", {name: "取消编辑", exact: true}).isDisabled(), true);
        }
        // A native click on a disabled control must not start a second mutation.
        await page.getByRole("button", {name: "恢复此版本", exact: true}).evaluate((button) => button.click());
        held.release();
        await page.getByRole("heading", {name: "v2", exact: true}).waitFor();
        const versions = await request(owner, "GET", path + "/versions");
        assert.equal(versions.length, 2);
        assert.equal(versions[1].question.title, action === "versions" ? "已提交的版本草稿" : "并发操作回归");
        assert.equal(await page.getByRole("button", {name: "保存共享设置", exact: true}).isEnabled(), true);
      });
    }

    await t.test("sharing locks input, shows canonical saved names and advances its revision", async (t) => {
      const {page, path} = await fixture(t);
      const held = await holdMutation(t, page, path + "/sharing", "PUT");
      const names = page.getByLabel("共享用户名", {exact: true});
      await names.fill("race_reader, race_owner, race_reader");
      await page.getByRole("button", {name: "保存共享设置", exact: true}).click();
      const payload = await held.received;
      assert.equal(await names.isDisabled(), true);
      assert.equal(payload.sharingRevision, 0);
      assert.equal(await page.getByRole("button", {name: "基于此版本编辑", exact: true}).isDisabled(), true);
      held.release();
      await page.getByText("共享设置已保存", {exact: true}).waitFor();
      assert.equal(await names.inputValue(), "race_reader");
      assert.equal(await names.isEnabled(), true);
      const saved = await request(owner, "GET", path);
      assert.equal(saved.sharingRevision, 1);
      assert.deepEqual(saved.sharedUsernames, ["race_reader"]);
      await names.fill("");
      const responsePromise = page.waitForResponse((response) =>
        response.url() === baseURL + path + "/sharing" && response.request().method() === "PUT");
      await page.getByRole("button", {name: "保存共享设置", exact: true}).click();
      const response = await responsePromise;
      assert.equal(response.status(), 200, await response.text());
      assert.equal(response.request().postDataJSON().sharingRevision, 1);
      assert.equal((await response.json()).sharingRevision, 2);
      await request(reader, "GET", path, undefined, 404);
    });

    await t.test("a stale page cannot restore access revoked in another tab", async (t) => {
      const {page, path} = await fixture(t);
      await page.getByLabel("共享用户名", {exact: true}).fill("race_reader");
      await page.getByRole("button", {name: "保存共享设置", exact: true}).click();
      await page.getByText("共享设置已保存", {exact: true}).waitFor();
      const other = await owner.newPage();
      t.after(() => other.close());
      other.setDefaultTimeout(10000);
      await other.goto(page.url());
      await other.getByLabel("共享用户名", {exact: true}).waitFor();
      assert.equal(await other.getByLabel("共享用户名", {exact: true}).inputValue(), "race_reader");
      await page.getByLabel("共享用户名", {exact: true}).fill("");
      const revoked = page.waitForResponse((response) =>
        response.url() === baseURL + path + "/sharing" && response.request().method() === "PUT");
      await page.getByRole("button", {name: "保存共享设置", exact: true}).click();
      assert.equal((await revoked).status(), 200);
      await request(reader, "GET", path, undefined, 404);
      await other.getByLabel("共享用户名", {exact: true}).fill("race_reader, race_owner");
      const responsePromise = other.waitForResponse((response) =>
        response.url() === baseURL + path + "/sharing" && response.request().method() === "PUT");
      await other.getByRole("button", {name: "保存共享设置", exact: true}).click();
      const response = await responsePromise;
      assert.equal(response.status(), 409, await response.text());
      await other.locator("#notice").filter({hasText: /刷新/}).waitFor();
      assert.equal(await other.getByLabel("共享用户名", {exact: true}).inputValue(), "race_reader, race_owner");
      assert.equal(await other.getByLabel("共享用户名", {exact: true}).isEnabled(), true);
      assert.deepEqual((await request(owner, "GET", path)).sharedWith, []);
      await request(reader, "GET", path, undefined, 404);
    });
    await t.test("restore protects title, note and sharing drafts until discard is confirmed", async (t) => {
      for (const field of ["题目标题", "版本说明", "共享用户名"]) {
        const {page, path} = await fixture(t);
        t.after(() => page.close());
        if (field !== "共享用户名")
          await page.getByRole("button", {name: "基于此版本编辑", exact: true}).click();
        const draft = field === "共享用户名" ? "race_reader" : "尚未保存的内容";
        await page.getByLabel(field, {exact: true}).fill(draft);
        const restore = () => page.getByRole("button", {name: "恢复此版本", exact: true}).click();
        const mutations = [];
        page.on("request", (req) => {
          if (req.method() === "POST" && req.url() === baseURL + path + "/restore") mutations.push(req);
        });
        await answerConfirmation(page, restore, false);
        assert.equal(await page.getByLabel(field, {exact: true}).inputValue(), draft);
        assert.equal((await request(owner, "GET", path + "/versions")).length, 1);
        assert.equal(mutations.length, 0, "cancel must not send a restore request");
        await request(reader, "GET", path, undefined, 404);

        const restored = page.waitForResponse((response) =>
          response.url() === baseURL + path + "/restore" && response.request().method() === "POST");
        await answerConfirmation(page, restore, true);
        assert.equal((await restored).status(), 201);
        await page.getByRole("heading", {name: "v2", exact: true}).waitFor();
        const versions = await request(owner, "GET", path + "/versions");
        assert.equal(versions.length, 2);
        assert.deepEqual(versions[1].question, versions[0].question);
        assert.equal(versions[1].restoredFromVersionId, versions[0].id);
        assert.deepEqual((await request(owner, "GET", path)).sharedUsernames || [], []);
        await request(reader, "GET", path, undefined, 404);
      }
    });

    await t.test("changing the editing baseline and cancelling preserve rejected drafts and ignore reverted edits", async (t) => {
      const {page, path} = await fixture(t);
      t.after(() => page.close());
      const edit = () => page.getByRole("button", {name: "基于此版本编辑", exact: true}).click();
      const cancel = () => page.getByRole("button", {name: "取消编辑", exact: true}).click();
      await withoutConfirmation(page, edit);
      await page.getByLabel("版本说明", {exact: true}).fill("保留版本说明");
      await answerConfirmation(page, edit, false);
      assert.equal(await page.getByLabel("版本说明", {exact: true}).inputValue(), "保留版本说明");
      await answerConfirmation(page, cancel, false);
      assert.equal(await page.getByLabel("版本说明", {exact: true}).inputValue(), "保留版本说明");
      await answerConfirmation(page, edit, true);
      assert.equal(await page.getByLabel("版本说明", {exact: true}).inputValue(), "");
      await page.getByLabel("题目标题", {exact: true}).fill("暂时修改");
      await page.getByLabel("题目标题", {exact: true}).fill("并发操作回归");
      await withoutConfirmation(page, cancel);
      assert.equal(await page.getByLabel("题目标题", {exact: true}).isVisible(), false);
      await withoutConfirmation(page, edit);
      await page.getByLabel("版本说明", {exact: true}).fill("确定放弃的说明");
      await answerConfirmation(page, cancel, true);
      assert.equal(await page.getByLabel("题目标题", {exact: true}).isVisible(), false);
      await page.getByLabel("共享用户名", {exact: true}).fill("race_reader");
      await page.getByLabel("共享用户名", {exact: true}).fill("");
      await withoutConfirmation(page, async () => {
        await page.getByRole("button", {name: "恢复此版本", exact: true}).click();
        await page.getByRole("heading", {name: "v2", exact: true}).waitFor();
      });
      assert.equal((await request(owner, "GET", path + "/versions")).length, 2);
    });

    await t.test("a cancelled version save keeps both drafts and saving sharing preserves the version draft", async (t) => {
      const {page, path} = await fixture(t);
      t.after(() => page.close());
      await page.getByRole("button", {name: "基于此版本编辑", exact: true}).click();
      await page.getByLabel("题目标题", {exact: true}).fill("保留并最终提交的版本");
      await page.getByLabel("版本说明", {exact: true}).fill("原草稿说明");
      await page.getByLabel("共享用户名", {exact: true}).fill("race_reader");
      const save = () => page.getByRole("button", {name: "保存新版本", exact: true}).click();
      const versionPosts = [];
      page.on("request", (req) => {
        if (req.method() === "POST" && req.url() === baseURL + path + "/versions") versionPosts.push(req);
      });
      await answerConfirmation(page, save, false);
      assert.equal(versionPosts.length, 0);
      assert.equal((await request(owner, "GET", path + "/versions")).length, 1);
      assert.equal(await page.getByLabel("题目标题", {exact: true}).inputValue(), "保留并最终提交的版本");
      assert.equal(await page.getByLabel("版本说明", {exact: true}).inputValue(), "原草稿说明");
      assert.equal(await page.getByLabel("共享用户名", {exact: true}).inputValue(), "race_reader");
      await request(reader, "GET", path, undefined, 404);
      await withoutConfirmation(page, async () => {
        await page.getByRole("button", {name: "保存共享设置", exact: true}).click();
        await page.getByText("共享设置已保存", {exact: true}).waitFor();
      });
      assert.equal(await page.getByLabel("题目标题", {exact: true}).inputValue(), "保留并最终提交的版本");
      assert.equal(await page.getByLabel("版本说明", {exact: true}).inputValue(), "原草稿说明");
      await request(reader, "GET", path);
      await withoutConfirmation(page, async () => {
        await save();
        await page.getByRole("heading", {name: "v2", exact: true}).waitFor();
      });
      const versions = await request(owner, "GET", path + "/versions");
      assert.equal(versions.length, 2);
      assert.equal(versions[1].question.title, "保留并最终提交的版本");
      assert.equal(versions[1].note, "原草稿说明");
      assert.deepEqual((await request(owner, "GET", path)).sharedUsernames, ["race_reader"]);
    });
    assert.deepEqual(errors, []);
  });
