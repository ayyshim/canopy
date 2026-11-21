## technology.backend
- Check every endpoint for input validation, auth, and the status code it returns on failure.
- Keep database changes as new migrations; never edit one already applied.
- Watch for slow queries, missing indexes and calls inside loops.
- Test with a real database where you can, including the error paths.
- Note any API shape change so the web app and docs get updated.

## technology.frontend
- Run the page in a browser and click through it; a passing build is not enough.
- Check loading, empty and error states, keyboard use and a phone-sized screen.
- Reuse existing components and styles before writing new ones.
- Keep copy and labels consistent with the rest of the app.
- Share a screenshot of the changed screen.

## technology.mobile
- Test on both a small and a large screen, in portrait and landscape.
- Handle no network, slow network and the app being sent to the background.
- Follow each platform's own conventions for navigation and permissions.
- Ask for a permission only when it is needed, with a clear reason.
- Note which OS versions you checked.

## technology.devops
- Make changes repeatable: put them in code or scripts, not one-off commands.
- Never put secrets in the repo or logs.
- Prefer a dry run or a staging check before touching anything live.
- Write down how to roll back before you deploy.
- Ask a person before any step that deletes data or costs money.

## technology.data_eng
- Make pipeline steps safe to re-run without duplicating rows.
- Check row counts, nulls and duplicates before and after each step.
- Keep schema changes backward compatible, and say who reads the data.
- Test with a small sample first, then the full set.
- Document each table: what it holds, where it comes from, how fresh it is.

## technology.ml
- Define what "good" means with a number and a fixed test set before you change anything.
- Change one thing at a time and compare against the previous result.
- Keep the data you test on separate from the data you tune on.
- Read real outputs, including the bad ones, not only the average score.
- Report the score, the method and the known weak spots.

## technology.qa
- Read what the feature should do, then list cases: normal, edge, and failure.
- Try to break it: empty input, huge input, odd characters, double clicks.
- For each bug, give exact steps, what you expected and what happened.
- Automate the checks that will be repeated.
- Deliver a test plan or bug list, ordered by how much each bug hurts.

## technology.security
- Check how data enters (input), who may act (auth) and what leaves (logs, errors).
- Look for injection, broken access checks, exposed secrets and unsafe dependencies.
- Confirm a finding by showing how it could be triggered, without causing harm.
- Rate each finding by impact and ease, and suggest a fix.
- Ask a person before testing anything you don't have permission to test.
