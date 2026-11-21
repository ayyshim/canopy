// References to goals, tasks and notes: G-12, T-34, N-5, with an optional leading #.
// The rules match hub/internal/refs: any case, not glued to other word characters, and not in code.

export type RefKind = "goal" | "task" | "note";
export type Ref = { kind: RefKind; number: number; label: string };

const LETTER: Record<string, RefKind> = { G: "goal", T: "task", N: "note" };
const PREFIX: Record<RefKind, string> = { goal: "G", task: "T", note: "N" };

/** refLabel is how an item is written: G-12. */
export const refLabel = (kind: RefKind, number: number) => `${PREFIX[kind]}-${number}`;

// A ref is a letter, a dash and digits, not glued to other word characters.
const REF = String.raw`(?<![\w/-])#?([GTNgtn])-(\d{1,9})\b`;
const EXACT = /^#?([GTNgtn])-(\d{1,9})$/;

const mk = (letter: string, digits: string): Ref | null => {
  const number = Number(digits);
  if (!(number >= 1)) return null;
  const kind = LETTER[letter.toUpperCase()];
  return { kind, number, label: refLabel(kind, number) };
};

/** parseRef reads one whole ref such as "T-34" or "#t-34". */
export function parseRef(s: string): Ref | null {
  const m = EXACT.exec(s.trim());
  return m ? mk(m[1], m[2]) : null;
}

export type Piece = string | { ref: Ref; text: string };

/** splitRefs cuts plain text into text and refs. Code spans and fenced blocks stay text. */
export function splitRefs(text: string): Piece[] {
  const out: Piece[] = [];
  const codeRe = /```[\s\S]*?(?:```|$)|`[^`\n]*`/g;
  let at = 0;
  const plain = (s: string) => {
    const re = new RegExp(REF, "g");
    let last = 0;
    for (let m = re.exec(s); m; m = re.exec(s)) {
      const ref = mk(m[1], m[2]);
      if (!ref) continue;
      if (m.index > last) out.push(s.slice(last, m.index));
      out.push({ ref, text: m[0] });
      last = m.index + m[0].length;
    }
    if (last < s.length) out.push(s.slice(last));
  };
  for (let m = codeRe.exec(text); m; m = codeRe.exec(text)) {
    plain(text.slice(at, m.index));
    out.push(m[0]);
    at = m.index + m[0].length;
  }
  plain(text.slice(at));
  return out;
}

/** findRefs lists the distinct refs in text, in order of first appearance. */
export function findRefs(text: string): Ref[] {
  const seen = new Set<string>();
  const out: Ref[] = [];
  for (const p of splitRefs(text)) {
    if (typeof p !== "string" && !seen.has(p.ref.label)) { seen.add(p.ref.label); out.push(p.ref); }
  }
  return out;
}

type Numbered = { id: string; number: number };
export type RefBoard = { goals: Numbered[]; tasks: Numbered[]; notes: Numbered[] };

/** itemOf finds the item a ref names among what the board shows. Hidden and unknown refs give undefined. */
export function itemOf(b: RefBoard | null | undefined, ref: Ref): { kind: RefKind; id: string } | undefined {
  if (!b) return undefined;
  const list = ref.kind === "goal" ? b.goals : ref.kind === "task" ? b.tasks : b.notes;
  const it = list.find((x) => x.number === ref.number);
  return it ? { kind: ref.kind, id: it.id } : undefined;
}

// A minimal view of the Markdown tree, enough to find text outside code and links.
type MdNode = { type: string; value?: string; url?: string; children?: MdNode[] };
const SKIP = new Set(["code", "inlineCode", "link", "linkReference", "definition", "html", "image", "imageReference"]);

/** remarkRefs turns refs in Markdown text into links to "#ref:T-34", for the ones isKnown accepts. */
export function remarkRefs(isKnown: (ref: Ref) => boolean) {
  const walk = (node: MdNode) => {
    if (!node.children || SKIP.has(node.type)) return;
    const next: MdNode[] = [];
    for (const child of node.children) {
      if (child.type !== "text" || !child.value) { walk(child); next.push(child); continue; }
      for (const p of splitRefs(child.value)) {
        if (typeof p === "string") next.push({ type: "text", value: p });
        else if (isKnown(p.ref)) next.push({ type: "link", url: "#ref:" + p.ref.label, children: [{ type: "text", value: p.text }] });
        else next.push({ type: "text", value: p.text });
      }
    }
    node.children = next;
  };
  return () => (tree: MdNode) => walk(tree);
}

/** refOfHref reads the ref out of a "#ref:T-34" link made by remarkRefs. */
export const refOfHref = (href: string | undefined): Ref | null => (href?.startsWith("#ref:") ? parseRef(href.slice(5)) : null);

/** refQueryAt finds an open "#query" just before the caret: the start of the "#" and what follows it. */
export function refQueryAt(text: string, caret: number): { start: number; query: string } | null {
  const m = /(?<![\w/-])#([\w-]{0,30})$/.exec(text.slice(0, caret));
  return m ? { start: m.index, query: m[1] } : null;
}
