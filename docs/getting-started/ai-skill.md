# AI Skill

The GLUT skill teaches an AI assistant how to write, review, and fix GLUT test
files.

The skill file is at [`skill/SKILL.md`](https://github.com/pandasoft-zz/glut/blob/main/skill/SKILL.md)
in the repository.

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
