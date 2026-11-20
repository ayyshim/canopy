// The setup wizard's state is the exact payload POSTed when someone presses Create workspace
// (SPEC: "the POST payload matches the review page exactly").
import type { Kind, Mode, SetupDir } from "./suggest";

/** A directory picked in setup, with what the computer said about it. A git repository counts
 * as a repository only with a remote to share through; without one (local_git) it syncs as a folder. */
export type PickedDir = SetupDir & { is_repo: boolean; local_git?: boolean; remote: string; branch: string };

export type SetupGrant = { directory_key: string; mode: Mode; source: "suggested" | "human"; reason: string };

export type SetupMember = {
  key: string;
  name: string;
  kind: "manager" | "staff";
  skill: string;
  reports_to_key: string; // "" = independent (staff) or none (manager)
  cli: string;
  model: string;
  all_directories: boolean;
  grants: SetupGrant[];
  instructions: string;
  grants_by: "suggested" | "you";
  skill_keys: string[]; // library skills (SetupSkill keys) this member starts with
};

/** A skill added to the workspace library during setup. GitHub skills carry the markdown already fetched. */
export type SetupSkill = { key: string; name: string; source_kind: "md" | "github"; source_url: string; content: string };

export type SetupPerson = {
  user_id: string | null;
  email: string;
  name?: string; // display only
  role: "admin" | "staff";
  manager_keys: string[];
  independent_staff: boolean;
};

export type SetupCLI = { cli: string; enabled: boolean; mode: "subscription" | "api_key"; cap_tokens_per_day: number | null; api_key: string };

export type SetupPayload = {
  workspace: { name: string; about: string; goal_completion: "manual" | "auto" };
  runner_id: string | null;
  directories: SetupDir[];
  clis: SetupCLI[];
  skills: SetupSkill[];
  members: SetupMember[];
  people: SetupPerson[];
  first_goal: { manager_key: string; title: string; description: string; done_when: string } | null;
};

let seq = 0;
export const newKey = (prefix: string) => `${prefix}${Date.now().toString(36)}${(seq++).toString(36)}`;

export function emptyPayload(): SetupPayload {
  return {
    workspace: { name: "", about: "", goal_completion: "manual" },
    runner_id: null,
    directories: [],
    clis: [],
    skills: [],
    members: [],
    people: [],
    first_goal: null,
  };
}

export type { Kind, Mode, SetupDir };
