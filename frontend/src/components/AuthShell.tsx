import Link from "next/link";
import { Brand } from "@/components/board/icons";

const STORY = (
  <>
    <h2>Your coding agents, working as a team you can see.</h2>
    <ul>
      <li><b>Give goals to managers.</b> They plan the work and hand tasks to their staff.</li>
      <li><b>Answer where it happens.</b> Questions, approvals and reviews show up on the tree.</li>
      <li><b>Your CLIs, your computers.</b> Canopy runs Claude Code where your code lives and never sees your keys.</li>
    </ul>
  </>
);

/** AuthShell: the form on the left, the product story on dark green. */
export function AuthShell({ children, story = STORY }: { children: React.ReactNode; story?: React.ReactNode }) {
  return (
    <div className="auth">
      <div className="auth-main">
        <Link className="brand" href="/" aria-label="Canopy home"><Brand size={26} /><b>Canopy</b></Link>
        <div className="auth-center">{children}</div>
        <p className="fine">Canopy runs the agent CLIs you already use. Keys stay on your computers.</p>
      </div>
      <aside className="auth-art">{story}</aside>
    </div>
  );
}
