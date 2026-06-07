import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const infoPanelSource = readFileSync(
  new URL("../src/components/VideoInfoPanel.tsx", import.meta.url),
  "utf8"
);
const searchPanelSource = readFileSync(
  new URL("../src/components/SearchPanel.tsx", import.meta.url),
  "utf8"
);

test("detail tags link to tag filtered listing", () => {
  assert.match(infoPanelSource, /import \{ Link \} from "react-router-dom";/);
  assert.match(
    infoPanelSource,
    /to=\{`\/list\?tag=\$\{encodeURIComponent\(t\)\}`\}/
  );
});

test("site search advertises tag search", () => {
  assert.match(searchPanelSource, /搜索视频标题、作者或标签/);
});
