# AI Skill

The GLUT skill teaches an AI assistant how to write, review, and fix GLUT test
files.

The skill file is at [`skill/SKILL.md`](https://github.com/pandasoft-zz/glut/blob/main/skill/SKILL.md)
in the repository.

## Workflow

Once the skill is installed, use this loop to write and improve tests:

1. **Write an initial test.** Give the AI your CI job YAML and ask it to write
   a GLUT test file.

2. **Validate.** Run `glut lint --format=json ./tests/my-test.yml` and pass the
   JSON output to the AI. The AI reads `has_errors` and each `issues[].category`
   to decide which fixes to make.

3. **Check coverage.** Run `glut doctor --format=json ./tests/my-test.yml` and
   pass the output. The AI reads `hints` and adds stronger asserts (artifacts,
   git, API, binary) as suggested.

4. **Run.** Run `glut run ./tests/my-test.yml`. On failure, pass the stdout and
   stderr back to the AI.

5. **Iterate** until the test passes and `glut doctor` reports no hints.

A strong GLUT test asserts the side effect that matters — the artifact written,
the git commit pushed, the API call made, or the binary invoked. A test that
only checks `exit-status: 0` is weak. `glut doctor` will tell you when this is
the case.

Example prompt to start:

```
I have this GitLab CI job: [paste job YAML]
Write a GLUT test. Use `glut doctor --format=json` output to improve coverage.
```

## Claude Code

[Claude Code](https://claude.ai/code) loads skills from `.claude/skills/` in
your project. If your project already has GLUT tests committed, commit the skill
alongside them — everyone who clones the repo gets it automatically with no
extra steps:

```bash
mkdir -p .claude/skills/glut-tests
cp path/to/glut/skill/SKILL.md .claude/skills/glut-tests/SKILL.md
git add .claude/skills/
git commit -m "chore: add GLUT skill for Claude Code"
```

If you want to install it without cloning GLUT first, use `curl`:

```bash
mkdir -p .claude/skills/glut-tests
curl -sSL https://raw.githubusercontent.com/pandasoft-zz/glut/main/skill/SKILL.md \
  -o .claude/skills/glut-tests/SKILL.md
```

For a global install available in every project:

```bash
mkdir -p ~/.claude/skills/glut-tests
curl -sSL https://raw.githubusercontent.com/pandasoft-zz/glut/main/skill/SKILL.md \
  -o ~/.claude/skills/glut-tests/SKILL.md
```

Claude picks up new skill files immediately — no restart needed. You can invoke
the skill directly with `/glut-tests` or just ask about GLUT tests and
Claude activates it automatically.

## OpenAI Codex

[Codex](https://github.com/openai/codex) reads `AGENTS.md` from the repository
root. Append the skill content to that file and commit it:

```bash
curl -sSL https://raw.githubusercontent.com/pandasoft-zz/glut/main/skill/SKILL.md \
  >> AGENTS.md
git add AGENTS.md
git commit -m "chore: add GLUT skill for Codex"
```

Codex reads `AGENTS.md` automatically — no extra command needed.

## Keeping the Skill Up to Date

The skill file is versioned with the rest of GLUT. Re-run the install command
after a GLUT upgrade to get updated instructions, new assert fields, and current
lint feedback format.
