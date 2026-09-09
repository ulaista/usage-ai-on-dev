# Existing Project / Brownfield Engine

Project Brain treats work on an existing repository as a first-class development mode rather than assuming every task starts from a clean or newly created project.

## Developer request flow

```text
Developer task
    |
    v
Project mode detection
    |-- new project -> normal context/routing flow
    `-- existing project
            |
            v
       Recovery baseline
       - HEAD / branch
       - languages/frameworks
       - build/test/CI tools
       - pre-existing dirty worktree
       - BASE / USER_DIRTY ownership
            |
            v
       Mechanical impact discovery
       - affected files
       - related tests
       - related config
       - recent history
       - current diff
       - regression window for bugfixes
            |
            v
       Semantic/context evidence
       - cached Serena symbol queries
       - Repo Map
       - Context Capsule
            |
            v
       Adaptive Router
       - mechanical
       - local
       - local-verify
       - strong
            |
            v
       guarded execution / tests / verification capsule / context delta
```

## Ownership model

Every brownfield session records three logical layers:

- `BASE`: committed repository state at session start.
- `USER_DIRTY`: uncommitted developer changes that already existed when Project Brain started the task.
- `BRAIN_DELTA`: files that changed after the baseline was captured.

If a file in `USER_DIRTY` changes again after the baseline, it is reported as an ownership conflict. Guarded local execution is suppressed and the task requires conflict-aware strong review rather than implicit overwrite or revert.

## Bugfix mode

For tasks classified as bugfixes, Project Brain mechanically collects a bounded regression window from Git history around likely affected files. It also identifies related tests/config before AI routing. Regression repair therefore starts from repository evidence rather than a fresh model guess.

## Feature-extension mode

Existing feature work uses the same baseline and ownership protection, then relies on affected files, semantic evidence, tests, config and context compilation to build the smallest compatibility-aware work packet possible.

## Discovery contract

An existing project is not eligible for local bounded execution until the discovery contract is ready. At minimum Project Brain must have:

- repository base/head,
- repository files,
- task-related affected files.

If discovery is incomplete, the adaptive router hard-gates local inference and escalates to strong-model ownership instead of guessing.

## Interfaces

CLI:

```text
brain-core project-detect
brain-core project-begin <session-id> <task>
brain-core project-impact <session-id>
brain-core developer-flow <session-id> <task>   # plan only
brain-core developer-run <session-id> <task>    # guarded execution
brain-core local-run <task>                     # brownfield-aware legacy path
```

MCP:

```text
brain_project_detect
brain_project_begin
brain_project_impact
brain_dev_plan
brain_dev_run
brain_local_run
```

`brain_dev_plan` performs recovery, impact discovery, semantic/context collection and routing without executing local AI. `brain_dev_run` follows the same guarded path and only invokes the local worker when the route permits it.

`brain_local_run` also auto-detects an existing repository and performs a brownfield recovery baseline when a caller does not explicitly pass project-mode metadata, closing the legacy bypass path.

## AI-call accounting

The recovery phase is deterministic. Typical existing-project preparation performs roughly 10-20 mechanical operations before routing. Expected AI calls are surfaced in the developer-flow plan:

- `mechanical`: 0 local, 0 strong;
- `local`: 1 local, 0 strong;
- `local-verify`: 1 local, 1 strong verifier;
- `strong`: 0 local, 1 strong owner.

Repository discovery, ownership tracking, Git history, tests/config lookup and cached semantic work stay outside model inference wherever possible.
