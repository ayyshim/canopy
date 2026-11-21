import assert from "node:assert/strict";
import { test } from "node:test";
import { parseDeepLink, workspaceOf } from "./deeplink.ts";

const id = "01a1fce4-d05f-783d-8ead-c1a484ae3b65";

test("reads ids and ignores junk", () => {
  assert.deepEqual(parseDeepLink(`?goal=${id}&task=${id}&signal=${id}`), { goal: id, task: id, signal: id });
  assert.deepEqual(parseDeepLink("?goal=nope&x=1"), {});
  assert.deepEqual(parseDeepLink(""), {});
});

test("finds the workspace in a path", () => {
  assert.equal(workspaceOf(`/w/${id}`), id);
  assert.equal(workspaceOf("/account"), null);
});
