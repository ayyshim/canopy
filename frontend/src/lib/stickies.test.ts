import assert from "node:assert/strict";
import { test } from "node:test";
import { canChangeVisibility, canDeleteNote, canEditNote, centerSpot, dragged, editedByAgent, isPinned, stickyColor, stickyPreview } from "./stickies.ts";

const note = (o = {}) => ({ visibility: "shared", author_user_id: "u1", ...o });

test("a note with both coordinates is a sticky", () => {
  assert.equal(isPinned(note({ board_x: 0, board_y: 0 })), true);
  assert.equal(isPinned(note({ board_x: 5 })), false);
  assert.equal(isPinned(note()), false);
});

test("unknown colors fall back to yellow", () => {
  assert.equal(stickyColor("pink"), "pink");
  assert.equal(stickyColor("teal"), "yellow");
  assert.equal(stickyColor(undefined), "yellow");
});

test("the face shows the first lines only", () => {
  assert.equal(stickyPreview("a\nb\nc\nd\ne\nf\ng", 3), "a\nb\nc");
  assert.equal(stickyPreview("only\n\n\n"), "only");
  assert.equal(stickyPreview(""), "");
});

test("who may edit", () => {
  assert.equal(canEditNote(note(), "u1", false), true);
  assert.equal(canEditNote(note(), "u2", false), false);
  assert.equal(canEditNote(note(), "u2", true), true);
  assert.equal(canEditNote(note({ visibility: "private" }), "u2", true), false);
  assert.equal(canChangeVisibility(note(), "u1"), true);
  assert.equal(canChangeVisibility(note(), "u2"), false);
});

test("who may delete", () => {
  assert.equal(canDeleteNote(note(), "u1", false), true);
  assert.equal(canDeleteNote(note(), "u2", false), false);
  assert.equal(canDeleteNote(note(), "u2", true), true);
  assert.equal(canDeleteNote(note({ visibility: "private" }), "u2", true), false);
  assert.equal(canDeleteNote(note({ visibility: "private" }), "u1", false), true);
});

test("Edited by shows after an agent edit", () => {
  assert.equal(editedByAgent(note({ edited_by_member_id: "m1" })), true);
  assert.equal(editedByAgent(note({ edited_by_user_id: "u1" })), false);
});

test("a new sticky lands in the middle of the view", () => {
  // At zoom 0.5, with the tree shifted by (100, 50), an 800x600 view centers on tree point (600, 500).
  assert.deepEqual(centerSpot({ x: 100, y: 50, z: 0.5 }, 800, 600), { x: 500, y: 425 });
});

test("dragging is measured in tree units", () => {
  assert.deepEqual(dragged(100, 100, 50, -20, 0.5), { x: 200, y: 60 });
});
