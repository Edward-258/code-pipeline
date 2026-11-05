// 前端冒烟（§9.19 防再发）：jsdom 里真执行页面脚本 + 真 fetch 打引擎，逐页签点击断言渲染。
// 背景：T4.2 的模板反引号配对崩坏让整段 JS 死掉（页面空+全不可点），而 grep 门全绿——
// 因为验收只看字符串不执行 JS。本脚本补上"真执行"这一环（不需要真浏览器，Docker 端口发布坏时也能跑）。
//
// 用法（jsdom 装在任意工作目录，node 从 cwd 解析）：
//   mkdir -p /tmp/webtest && cd /tmp/webtest && npm init -y && npm install jsdom
//   NODE_TLS_REJECT_UNAUTHORIZED=0 node /path/to/mini-pipeline/deploy/web/smoke.js [BASE]
//     BASE 默认 https://pipeline.47.113.177.198.nip.io（自签证书需禁验）
// 退出码：断言全过 0，有红 1。
const { JSDOM } = require(process.cwd() + "/node_modules/jsdom");
const fs = require("fs");
const path = require("path");

const BASE = process.argv[2] || "https://pipeline.47.113.177.198.nip.io";
const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");

let fail = 0;
const ok = (name, cond) => { console.log(`  ${cond ? "✅" : "❌"} ${name}`); if (!cond) fail++; };

const dom = new JSDOM(html, { url: BASE + "/", runScripts: "outside-only", pretendToBeVisual: true });
const w = dom.window;
w.alert = () => {};
w.confirm = () => true;
w.fetch = (p, opt) => fetch(BASE + p, opt); // node18+ 原生 fetch；自签证书用 NODE_TLS_REJECT_UNAUTHORIZED=0
w.HTMLElement.prototype.scrollIntoView = function () {};

try { w.eval(html.match(/<script>([\s\S]*)<\/script>/)[1]); } catch (e) { console.log("❌ 脚本执行即抛:", e.message); process.exit(1); }
const sleep = ms => new Promise(r => setTimeout(r, ms));
const mainText = () => w.document.querySelector("#main").textContent;

(async () => {
  await sleep(2500);
  ok("初始页（工作流）出列表", mainText().includes("工作流列表"));
  ok("工作流行有真数据", w.document.querySelectorAll("#main tr.click").length > 0);

  for (const name of ["task", "env", "notify", "cron"]) {
    const btn = w.document.querySelector(`[data-t="${name}"]`);
    btn.click();
    await sleep(2500);
    ok(`页签[${name}]渲染非空`, mainText().trim().length > 20);
  }
  // 回到任务页签深一步：列表行 + 时间不是 Invalid Date + 详情树
  w.document.querySelector('[data-t="task"]').click();
  await sleep(2500);
  const rows = [...w.document.querySelectorAll("#taskRows tr.click")];
  if (rows.length) {
    ok("任务列表行在位", true);
    ok("时间戳可解析（无 Invalid Date）", !w.document.querySelector("#taskRows").textContent.includes("Invalid Date"));
    rows[0].click();
    await sleep(2500);
    ok("任务详情树渲染", w.document.querySelector("#taskDetail").textContent.includes("cancel"));
  } else {
    ok("任务列表行在位（空列表算过——新库）", true);
  }
  console.log(fail === 0 ? "前端冒烟全绿 ✅" : `前端冒烟有红 ❌ (${fail})`);
  process.exit(fail);
})();
