import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const adminCss = readFileSync(
  new URL("../src/styles/admin.css", import.meta.url),
  "utf8"
);

function ruleBody(css: string, selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `Expected CSS rule for ${selector}`);
  return match[1];
}

function allRuleBodies(css: string, selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return Array.from(css.matchAll(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`, "g")))
    .map((match) => match[1])
    .join("\n");
}

function mobileCss(): string {
  const marker = "@media (max-width: 768px)";
  const start = adminCss.indexOf(marker);
  assert.notEqual(start, -1, "Expected mobile admin media query");
  return adminCss.slice(start);
}

test("admin login card fits narrow phone screens", () => {
  const body = ruleBody(adminCss, ".admin-login__card");

  assert.match(body, /width\s*:\s*min\(360px,\s*100%\)/);
  assert.match(body, /box-sizing\s*:\s*border-box/);
});

test("admin tables scroll inside the mobile viewport", () => {
  const css = mobileCss();
  const body = ruleBody(css, ".admin-table");

  assert.match(body, /display\s*:\s*block/);
  assert.match(body, /max-width\s*:\s*100%/);
  assert.match(body, /overflow-x\s*:\s*auto/);
});

test("admin modals and action footers adapt on mobile", () => {
  const css = mobileCss();

  assert.match(ruleBody(css, ".admin-modal"), /width\s*:\s*100%/);
  assert.match(allRuleBodies(css, ".admin-modal__footer"), /flex-wrap\s*:\s*wrap/);
  assert.match(ruleBody(css, ".admin-form"), /max-width\s*:\s*100%/);
});
