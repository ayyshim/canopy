package domain

import "strings"

// Base roles drive default folder access and planning.
var BaseRoles = []string{"developer", "reviewer", "researcher", "designer", "writer"}

type Subcategory struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Base  string `json:"base"`
}

type Category struct {
	Key   string        `json:"key"`
	Label string        `json:"label"`
	Subs  []Subcategory `json:"subs"`
}

func sub(key, label, desc, base string) Subcategory {
	return Subcategory{Key: key, Label: label, Desc: desc, Base: base}
}

// Catalog lists every "good at" a staff member can have. The first category holds the five
// original keys, which stay valid so existing members and tasks keep working.
var Catalog = []Category{
	{"general", "General", []Subcategory{
		sub("developer", "Developer", "Writes and changes code", "developer"),
		sub("reviewer", "Reviewer", "Reviews work and finds problems", "reviewer"),
		sub("researcher", "Researcher", "Finds and summarizes information", "researcher"),
		sub("designer", "Designer", "Designs interfaces and visuals", "designer"),
		sub("writer", "Writer", "Writes docs and copy", "writer"),
	}},
	{"technology", "Technology", []Subcategory{
		sub("technology.backend", "Backend", "APIs, services and databases", "developer"),
		sub("technology.frontend", "Frontend", "Web interfaces and client code", "developer"),
		sub("technology.mobile", "Mobile apps", "iOS and Android apps", "developer"),
		sub("technology.devops", "DevOps", "Deploys, CI and infrastructure", "developer"),
		sub("technology.data_eng", "Data engineering", "Pipelines and data stores", "developer"),
		sub("technology.ml", "Machine learning", "Models, prompts and evaluation", "developer"),
		sub("technology.qa", "QA and testing", "Test plans, automation and bug hunting", "reviewer"),
		sub("technology.security", "Security", "Audits code and finds vulnerabilities", "reviewer"),
	}},
	{"marketing", "Marketing", []Subcategory{
		sub("marketing.seo", "SEO", "Search ranking and keyword strategy", "researcher"),
		sub("marketing.content", "Content marketing", "Blog posts, guides and newsletters", "writer"),
		sub("marketing.social", "Social media", "Posts and community engagement", "writer"),
		sub("marketing.email", "Email campaigns", "Email sequences and lifecycle messages", "writer"),
		sub("marketing.paid", "Paid ads", "Ad copy, targeting and budgets", "researcher"),
		sub("marketing.brand", "Branding", "Brand voice and identity", "designer"),
		sub("marketing.analytics", "Marketing analytics", "Campaign metrics and reports", "researcher"),
	}},
	{"sales", "Sales", []Subcategory{
		sub("sales.outreach", "Outreach", "Cold emails and prospect lists", "writer"),
		sub("sales.proposals", "Proposals", "Quotes, decks and proposals", "writer"),
		sub("sales.leadgen", "Lead research", "Finds and qualifies leads", "researcher"),
		sub("sales.crm", "CRM upkeep", "Keeps pipeline data clean", "researcher"),
		sub("sales.enablement", "Sales enablement", "Playbooks and battle cards", "writer"),
		sub("sales.partnerships", "Partnerships", "Partner research and pitches", "researcher"),
	}},
	{"film", "Film Making", []Subcategory{
		sub("film.screenwriting", "Screenwriting", "Scripts and treatments", "writer"),
		sub("film.storyboard", "Storyboarding", "Shot lists and storyboards", "designer"),
		sub("film.direction", "Direction notes", "Creative direction and scene breakdowns", "writer"),
		sub("film.editing", "Editing plans", "Edit decision lists and pacing notes", "designer"),
		sub("film.sound", "Sound design", "Sound and score cues", "designer"),
		sub("film.production", "Production planning", "Schedules, budgets and locations", "researcher"),
		sub("film.critique", "Film critique", "Reviews cuts and scripts", "reviewer"),
	}},
	{"design", "Design", []Subcategory{
		sub("design.ui", "UI design", "Screens and components", "designer"),
		sub("design.ux", "UX research", "User flows and usability", "researcher"),
		sub("design.graphic", "Graphic design", "Posters, banners and social art", "designer"),
		sub("design.motion", "Motion design", "Animation and transitions", "designer"),
		sub("design.illustration", "Illustration", "Icons and illustrations", "designer"),
		sub("design.systems", "Design systems", "Tokens, components and guidelines", "designer"),
		sub("design.review", "Design review", "Checks designs for polish and consistency", "reviewer"),
	}},
	{"writing", "Writing & Content", []Subcategory{
		sub("writing.copy", "Copywriting", "Short, persuasive copy", "writer"),
		sub("writing.technical", "Technical writing", "Docs, guides and API references", "writer"),
		sub("writing.editing", "Editing", "Edits drafts for clarity and tone", "reviewer"),
		sub("writing.blog", "Blogging", "Articles and thought pieces", "writer"),
		sub("writing.translation", "Translation", "Translates and localizes text", "writer"),
		sub("writing.creative", "Creative writing", "Stories and narrative", "writer"),
		sub("writing.speech", "Speeches and scripts", "Talks, podcasts and video scripts", "writer"),
	}},
	{"research", "Research & Data", []Subcategory{
		sub("research.market", "Market research", "Markets, trends and competitors", "researcher"),
		sub("research.competitive", "Competitive analysis", "Compares products and pricing", "researcher"),
		sub("research.data_analysis", "Data analysis", "Finds patterns in data", "researcher"),
		sub("research.visualization", "Data visualization", "Charts and dashboards", "designer"),
		sub("research.academic", "Academic research", "Papers, sources and citations", "researcher"),
		sub("research.survey", "Surveys", "Designs and summarizes surveys", "researcher"),
		sub("research.factcheck", "Fact checking", "Verifies claims and sources", "reviewer"),
	}},
	{"product", "Product", []Subcategory{
		sub("product.management", "Product management", "Roadmaps and priorities", "writer"),
		sub("product.requirements", "Requirements", "Specs and user stories", "writer"),
		sub("product.discovery", "Product discovery", "Customer problems and opportunities", "researcher"),
		sub("product.analytics", "Product analytics", "Usage metrics and funnels", "researcher"),
		sub("product.growth", "Growth", "Experiments to grow usage", "researcher"),
		sub("product.release", "Release notes", "Changelogs and launch notes", "writer"),
	}},
	{"finance", "Finance", []Subcategory{
		sub("finance.accounting", "Accounting", "Books, ledgers and reconciliation", "researcher"),
		sub("finance.budgeting", "Budgeting", "Budgets and forecasts", "researcher"),
		sub("finance.modeling", "Financial modeling", "Models and scenarios", "researcher"),
		sub("finance.reporting", "Financial reporting", "Statements and board reports", "writer"),
		sub("finance.invoicing", "Invoicing", "Invoices and payment follow-up", "writer"),
		sub("finance.audit", "Audit", "Checks records for errors", "reviewer"),
	}},
	{"legal", "Legal", []Subcategory{
		sub("legal.contracts", "Contracts", "Drafts and summarizes contracts", "writer"),
		sub("legal.review", "Contract review", "Flags risky clauses", "reviewer"),
		sub("legal.compliance", "Compliance", "Checks rules and regulations", "reviewer"),
		sub("legal.privacy", "Privacy", "Privacy policies and data rules", "writer"),
		sub("legal.ip", "Intellectual property", "Trademarks, licenses and patents", "researcher"),
		sub("legal.research", "Legal research", "Case law and statutes", "researcher"),
	}},
	{"operations", "Operations", []Subcategory{
		sub("operations.process", "Process design", "Maps and improves workflows", "writer"),
		sub("operations.project", "Project management", "Plans, timelines and status", "writer"),
		sub("operations.procurement", "Procurement", "Vendors and purchasing", "researcher"),
		sub("operations.logistics", "Logistics", "Shipping and supply chain", "researcher"),
		sub("operations.docs", "Runbooks", "Checklists and runbooks", "writer"),
		sub("operations.quality", "Quality control", "Audits processes for defects", "reviewer"),
	}},
	{"support", "Customer Support", []Subcategory{
		sub("support.tickets", "Ticket replies", "Answers customer questions", "writer"),
		sub("support.kb", "Knowledge base", "Help articles and FAQs", "writer"),
		sub("support.triage", "Triage", "Sorts and prioritizes requests", "reviewer"),
		sub("support.feedback", "Feedback analysis", "Finds themes in customer feedback", "researcher"),
		sub("support.onboarding", "Customer onboarding", "Welcome guides and walkthroughs", "writer"),
		sub("support.success", "Customer success", "Account health and renewals", "researcher"),
	}},
	{"hr", "HR & People", []Subcategory{
		sub("hr.recruiting", "Recruiting", "Sources and screens candidates", "researcher"),
		sub("hr.jobposts", "Job posts", "Job descriptions and ads", "writer"),
		sub("hr.onboarding", "Employee onboarding", "Onboarding plans and handbooks", "writer"),
		sub("hr.policy", "People policy", "Policies and handbooks", "writer"),
		sub("hr.performance", "Performance reviews", "Review templates and feedback", "reviewer"),
		sub("hr.culture", "Culture and engagement", "Surveys and team events", "researcher"),
	}},
	{"education", "Education", []Subcategory{
		sub("education.curriculum", "Curriculum design", "Course outlines and goals", "writer"),
		sub("education.lessons", "Lesson plans", "Lessons and activities", "writer"),
		sub("education.tutoring", "Tutoring", "Explains topics step by step", "writer"),
		sub("education.assessment", "Assessment", "Quizzes, rubrics and grading", "reviewer"),
		sub("education.elearning", "E-learning", "Online course material", "designer"),
		sub("education.research", "Learning research", "Teaching methods and studies", "researcher"),
	}},
	{"music", "Music & Audio", []Subcategory{
		sub("music.composition", "Composition", "Melodies, chords and arrangements", "writer"),
		sub("music.lyrics", "Lyrics", "Songwriting and lyrics", "writer"),
		sub("music.production", "Music production", "Mixing and production notes", "designer"),
		sub("music.podcast", "Podcasting", "Episode plans and show notes", "writer"),
		sub("music.sounddesign", "Sound design", "Effects and audio branding", "designer"),
		sub("music.theory", "Music theory", "Analysis and research", "researcher"),
	}},
	{"creative", "Creative & Lifestyle", []Subcategory{
		sub("creative.gaming", "Game design", "Mechanics, levels and balance", "designer"),
		sub("creative.photography", "Photography", "Shot planning and photo editing notes", "designer"),
		sub("creative.events", "Event planning", "Agendas, venues and run of show", "writer"),
		sub("creative.travel", "Travel planning", "Itineraries and bookings research", "researcher"),
		sub("creative.food", "Food and recipes", "Recipes and menu planning", "writer"),
		sub("creative.fitness", "Fitness coaching", "Workout and nutrition plans", "writer"),
		sub("creative.architecture", "Architecture and interiors", "Layouts and space concepts", "designer"),
	}},
	{"science", "Science & Environment", []Subcategory{
		sub("science.lab", "Lab research", "Experiment design and notes", "researcher"),
		sub("science.climate", "Climate and sustainability", "Impact studies and reports", "researcher"),
		sub("science.health", "Health and wellness", "Summarizes health research", "researcher"),
		sub("science.grants", "Grant writing", "Funding proposals", "writer"),
		sub("science.peerreview", "Peer review", "Checks methods and results", "reviewer"),
	}},
}

var subByKey = func() map[string]Subcategory {
	m := map[string]Subcategory{}
	for _, c := range Catalog {
		for _, s := range c.Subs {
			m[s.Key] = s
		}
	}
	return m
}()

// ValidSkill reports whether s is a catalog key (a base role or a subcategory).
func ValidSkill(s string) bool {
	_, ok := subByKey[s]
	return ok
}

// BaseRole returns the base role behind a skill key, or "" when the key is unknown.
func BaseRole(skill string) string {
	return subByKey[skill].Base
}

// SkillLabel returns the label for a key, or the key itself when unknown.
func SkillLabel(skill string) string {
	if s, ok := subByKey[skill]; ok {
		return s.Label
	}
	return skill
}

// SkillKeys lists every valid key, for error messages.
func SkillKeys() []string {
	var out []string
	for _, c := range Catalog {
		for _, s := range c.Subs {
			out = append(out, s.Key)
		}
	}
	return out
}

// SkillMatches reports whether a member's skill fits what a task asks for: an exact key,
// or the same base role when the task asks for a plain base role.
func SkillMatches(have, want string) bool {
	if have == "" || want == "" {
		return false
	}
	if have == want {
		return true
	}
	return want == BaseRole(want) && BaseRole(have) == want
}

// CatalogSummary is a compact catalog for prompts: "category: key (base), ...".
func CatalogSummary() string {
	var b strings.Builder
	for _, c := range Catalog {
		if c.Key == "general" {
			continue
		}
		b.WriteString("- " + c.Label + ": ")
		for i, s := range c.Subs {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(s.Key)
		}
		b.WriteString("\n")
	}
	return b.String()
}
