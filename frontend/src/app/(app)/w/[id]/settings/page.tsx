import { WorkspaceSettings } from "./WorkspaceSettings";

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function WorkspaceSettingsPage() {
  return <WorkspaceSettings />;
}
