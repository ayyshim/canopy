// Shapes the hub returns beyond the generated core types.
import type {
  AccountView, DirectoryView, GoalEventView, GoalView, GrantView, MemberView, OutputView, SessionView, SignalView, TaskView, WorkspaceView,
  ChatMessageView, LogLineView, RunnerView, CLIStatus, CloneView, GoalRepoView, NoteView,
} from "./core-types";

export type {
  AccountView, DirectoryView, GoalEventView, GoalView, GrantView, MemberView, OutputView, SessionView, SignalView, TaskView, WorkspaceView,
  ChatMessageView, LogLineView, RunnerView, CLIStatus, CloneView, GoalRepoView, NoteView,
};

export type HiddenMember = { id: string; kind: string; reports_to?: string | null; hidden: true; position: number };
export type AnyMember = MemberView | HiddenMember;
export const isHidden = (m: AnyMember | undefined): m is HiddenMember => !!m && (m as HiddenMember).hidden === true;

export type Viewer = { role: "admin" | "staff"; org_role: string; manager_ids: string[]; independent: boolean };

export type DirectoryDetail = DirectoryView & { owner_name: string; your_mode: string; access?: Record<string, string> };

export type MirrorState = { revision?: number; complete?: boolean; missing?: boolean; last_synced_at?: string | null; queued_writes?: number };

export type Person = { id: string; name: string; avatar_color: string };

/** A computer (runner) as the board shows it. */
export type Computer = RunnerView & { clis: CLIStatus[] };

/** Everything the board draws, filtered by the hub to what the viewer may see. */
export type Board = {
  seq: number;
  workspace: WorkspaceView;
  viewer: Viewer;
  members: AnyMember[];
  grants: GrantView[];
  directories: DirectoryDetail[];
  clones: CloneView[];
  goals: GoalView[];
  goal_events: GoalEventView[];
  goal_repos: GoalRepoView[];
  tasks: TaskView[];
  signals: SignalView[];
  sessions: SessionView[];
  outputs: OutputView[];
  notes: NoteView[];
  seen: Record<string, number>;
  unread: Record<string, number>;
  accounts: AccountView[];
  runners: Computer[];
  computers: Record<string, string | null>; // person id → the computer their goals run on
  mirrors: Record<string, MirrorState>;
  people: Person[];
};

export type PresenceUser = { id: string; name: string; avatar_color: string };
export type Cursor = { user_id: string; name: string; avatar_color: string; x: number; y: number; at: number };

export type CLIModel = { id: string; label: string };
export type CLIInfo = { id: string; name: string; supported: boolean; install_cmd: string; models: CLIModel[]; manager_model: string; staff_model: string };

export type RunnerInfo = Computer;

export type WorkspaceSummary = WorkspaceView & { members: number; directories: number; people: number; role: string };

export const SKILLS = ["developer", "reviewer", "researcher", "designer", "writer"] as const;
export type Skill = (typeof SKILLS)[number];
export const SKILL_LABEL: Record<string, string> = {
  developer: "Developer", reviewer: "Reviewer", researcher: "Researcher", designer: "Designer", writer: "Writer",
};
export const SKILL_DESC: Record<string, string> = {
  developer: "Writes and changes code",
  reviewer: "Reviews code and outputs, finds problems",
  researcher: "Reads, compares and reports",
  designer: "Designs screens and flows",
  writer: "Writes docs, copy and notes",
};
