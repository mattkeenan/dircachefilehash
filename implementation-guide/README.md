# Implementation Guide

CWF (Coding with Files) workspace for **dircachefilehash**. Task plans, designs, and execution records live here.

## Project config

See [cwf-project.json](cwf-project.json) — project name, GitHub source/task management, branch conventions, task types.

## Workflow commands

Invoke via the `Skill` tool. Pass the task number, e.g. `/cwf-task-plan 98`.

| Stage | Skill | Purpose |
|-------|-------|---------|
| Task | `/cwf-new-task` · `/cwf-task-plan` (`a-`) | Create and scope a task |
| Requirements | `/cwf-requirements-plan` (`b-`) | Define requirements |
| Design | `/cwf-design-plan` (`c-`) | Design the solution |
| Implementation | `/cwf-implementation-plan` (`d-`) · `/cwf-implementation-exec` (`f-`) | Plan and execute code changes |
| Testing | `/cwf-testing-plan` (`e-`) · `/cwf-testing-exec` (`g-`) | Plan and run tests |
| Rollout | `/cwf-rollout` (`h-`) | Ship the change |
| Maintenance | `/cwf-maintenance` (`i-`) | Post-ship maintenance |
| Retrospective | `/cwf-retrospective` (`j-`) | Review what happened |

Support skills: `/cwf-status`, `/cwf-current-task`, `/cwf-new-subtask`, `/cwf-delete-task`, `/cwf-backlog-manager`, `/cwf-config`, `/cwf-extract`, `/cwf-security-check`.

## Layout

Each task gets a numbered directory under `implementation-guide/` containing its `a-` through `j-` workflow files.
