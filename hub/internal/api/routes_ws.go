package api

import (
	"github.com/go-chi/chi/v5"

	"canopy/hub/internal/httpx"
)

// workspaceRoutes are the signed-in organization and workspace endpoints.
func (s *Server) workspaceRoutes(r chi.Router) {
	r.Get("/clis", httpx.Handler(s.handleListCLIs))

	r.Put("/orgs/{orgID}/logo", httpx.Handler(s.handlePutOrgLogo))
	r.Get("/orgs/{orgID}/logo", httpx.Handler(s.handleGetOrgLogo))
	r.Delete("/orgs/{orgID}/logo", httpx.Handler(s.handleDeleteOrgLogo))
	r.Put("/workspaces/{wsID}/logo", httpx.Handler(s.handlePutWorkspaceLogo))
	r.Get("/workspaces/{wsID}/logo", httpx.Handler(s.handleGetWorkspaceLogo))
	r.Delete("/workspaces/{wsID}/logo", httpx.Handler(s.handleDeleteWorkspaceLogo))

	r.Get("/orgs/{orgID}/people", httpx.Handler(s.handleListPeople))
	r.Patch("/orgs/{orgID}/memberships/{userID}", httpx.Handler(s.handlePatchMembership))
	r.Delete("/orgs/{orgID}/memberships/{userID}", httpx.Handler(s.handleDeleteMembership))
	r.Get("/orgs/{orgID}/invites", httpx.Handler(s.handleListInvites))
	r.Post("/orgs/{orgID}/invites", httpx.Handler(s.handleCreateInvite))
	r.Delete("/orgs/{orgID}/invites/{inviteID}", httpx.Handler(s.handleRevokeInvite))

	// Computers and how their CLIs sign in.
	r.Get("/orgs/{orgID}/connections", httpx.Handler(s.handleListConnections))
	r.Get("/orgs/{orgID}/runners", httpx.Handler(s.handleListRunners))
	r.Put("/orgs/{orgID}/my-computer", httpx.Handler(s.handleSetMyComputer))
	r.Post("/orgs/{orgID}/runners/pairing", httpx.Handler(s.handleCreatePairing))
	r.Get("/orgs/{orgID}/runners/pairing/{pairingID}", httpx.Handler(s.handleGetPairing))
	r.Patch("/runners/{runnerID}", httpx.Handler(s.handlePatchRunner))
	r.Delete("/runners/{runnerID}", httpx.Handler(s.handleRevokeRunner))
	r.Put("/runners/{runnerID}/connections/{cli}", httpx.Handler(s.handlePutConnection))
	r.Get("/runners/{runnerID}/browse", httpx.Handler(s.handleBrowse))
	r.Get("/runners/{runnerID}/repo", httpx.Handler(s.handleRepoInfo))

	r.Get("/orgs/{orgID}/workspaces", httpx.Handler(s.handleListWorkspaces))
	r.Post("/orgs/{orgID}/workspaces", httpx.Handler(s.handleCreateWorkspace))
	r.Get("/workspaces/{wsID}", httpx.Handler(s.handleGetWorkspace))
	r.Get("/workspaces/{wsID}/board", httpx.Handler(s.handleBoard))
	r.Patch("/workspaces/{wsID}", httpx.Handler(s.handlePatchWorkspace))
	r.Delete("/workspaces/{wsID}", httpx.Handler(s.handleDeleteWorkspace))
	r.Post("/workspaces/{wsID}/restart-sessions", httpx.Handler(s.handleRestartSessions))

	r.Post("/workspaces/{wsID}/members", httpx.Handler(s.handleCreateMember))
	r.Patch("/workspaces/{wsID}/members/{memberID}", httpx.Handler(s.handlePatchMember))
	r.Delete("/workspaces/{wsID}/members/{memberID}", httpx.Handler(s.handleDeleteMember))
	r.Put("/workspaces/{wsID}/members/{memberID}/grants", httpx.Handler(s.handlePutGrants))
	r.Post("/workspaces/{wsID}/members/{memberID}/suggest-grants", httpx.Handler(s.handleSuggestGrants))

	r.Get("/workspaces/{wsID}/staff-proposals", httpx.Handler(s.handleListStaffProposals))
	r.Post("/workspaces/{wsID}/staff-proposals/{proposalID}/approve", httpx.Handler(s.handleApproveStaffProposal))
	r.Post("/workspaces/{wsID}/staff-proposals/{proposalID}/reject", httpx.Handler(s.handleRejectStaffProposal))

	r.Get("/workspaces/{wsID}/skills", httpx.Handler(s.handleListSkills))
	r.Post("/workspaces/{wsID}/skills", httpx.Handler(s.handleCreateSkill))
	r.Post("/orgs/{orgID}/skills/preview", httpx.Handler(s.handlePreviewSkill))
	r.Post("/orgs/{orgID}/setup/chat", httpx.Handler(s.handleSetupChat))
	r.Patch("/skills/{skillID}", httpx.Handler(s.handlePatchSkill))
	r.Delete("/skills/{skillID}", httpx.Handler(s.handleDeleteSkill))
	r.Get("/workspaces/{wsID}/members/{memberID}/skills", httpx.Handler(s.handleListMemberSkills))
	r.Put("/workspaces/{wsID}/members/{memberID}/skills/{skillID}", httpx.Handler(s.handleAttachSkill))
	r.Delete("/workspaces/{wsID}/members/{memberID}/skills/{skillID}", httpx.Handler(s.handleDetachSkill))

	r.Post("/workspaces/{wsID}/directories", httpx.Handler(s.handleCreateDirectory))
	r.Patch("/workspaces/{wsID}/directories/{dirID}", httpx.Handler(s.handlePatchDirectory))
	r.Delete("/workspaces/{wsID}/directories/{dirID}", httpx.Handler(s.handleDeleteDirectory))
	r.Put("/workspaces/{wsID}/directories/{dirID}/access/{userID}", httpx.Handler(s.handlePutDirectoryAccess))

	r.Get("/workspaces/{wsID}/notes", httpx.Handler(s.handleListNotes))
	r.Post("/workspaces/{wsID}/notes", httpx.Handler(s.handleCreateNote))
	r.Patch("/notes/{noteID}", httpx.Handler(s.handlePatchNote))
	r.Delete("/notes/{noteID}", httpx.Handler(s.handleDeleteNote))

	r.Post("/workspaces/{wsID}/goals", httpx.Handler(s.handleCreateGoal))
	r.Get("/goals/{goalID}", httpx.Handler(s.handleGetGoal))
	r.Patch("/goals/{goalID}", httpx.Handler(s.handlePatchGoal))
	r.Post("/goals/{goalID}/ship", httpx.Handler(s.handleShipGoal))
	r.Post("/goals/{goalID}/done", httpx.Handler(s.handleGoalDone))
	r.Post("/goals/{goalID}/move", httpx.Handler(s.handleMoveGoal))
	r.Post("/goals/{goalID}/tasks", httpx.Handler(s.handleCreateTask))
	r.Post("/workspaces/{wsID}/tasks", httpx.Handler(s.handleCreateTask))
	r.Get("/tasks/{taskID}", httpx.Handler(s.handleGetTask))
	r.Patch("/tasks/{taskID}", httpx.Handler(s.handlePatchTask))
	r.Post("/signals/{signalID}/resolve", httpx.Handler(s.handleResolveSignal))
	r.Get("/workspaces/{wsID}/members/{memberID}/activity", httpx.Handler(s.handleMemberActivity))
	r.Get("/workspaces/{wsID}/members/{memberID}/chat", httpx.Handler(s.handleGetChat))
	r.Post("/workspaces/{wsID}/members/{memberID}/chat", httpx.Handler(s.handlePostChat))
	r.Post("/workspaces/{wsID}/members/{memberID}/chat/read", httpx.Handler(s.handleMarkChatRead))
	r.Post("/tasks/{taskID}/comments", httpx.Handler(s.handlePostComment))

	r.Get("/outputs/{outputID}", httpx.Handler(s.handleGetOutput))
	r.Get("/outputs/{outputID}/versions/{n}/file", httpx.Handler(s.handleOutputFile))
	r.Post("/outputs/{outputID}/seen", httpx.Handler(s.handleOutputSeen))

	r.Get("/orgs/{orgID}/usage", httpx.Handler(s.handleOrgUsage))
	r.Get("/orgs/{orgID}/usage/claude", httpx.Handler(s.handleClaudeUsage))
	r.Get("/orgs/{orgID}/usage/people/{userID}", httpx.Handler(s.handlePersonUsage))
	r.Get("/orgs/{orgID}/activity", httpx.Handler(s.handleActivity))

	r.Get("/workspaces/{wsID}/access", httpx.Handler(s.handleListAccess))
	r.Put("/workspaces/{wsID}/access/{userID}", httpx.Handler(s.handlePutAccess))
	r.Delete("/workspaces/{wsID}/access/{userID}", httpx.Handler(s.handleDeleteAccess))
}
