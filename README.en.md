# CHASSISS

[中文](README.md) | English

CHASSISS is a CLI-centered software development workflow for multiple agents.

Agents make semantic decisions across requirements, architecture, implementation, and review. The CLI manages templates, authorization, state, task assignment, scope checks, concurrency, audit evidence, and recovery. A human Master keeps the root key, accepts critical designs, and may take over maintenance through an Owner credential. Every agent works with a role credential issued by the root key.

CHASSISS does not depend on GitHub, GitLab, or a specific hosting service. All participants only need a reliable way to synchronize the same formal project version. Version 0.3 currently uses local Git for baselines, worktrees, diffs, and integration.

CHASSISS is designed for cooperative agents that may still misunderstand, forget, or make mistakes. It provides a recoverable and auditable workflow. It does not provide a sandbox for malicious code.

## Quickstart

The recommended minimum has two resident agents. A Build Agent holds the Orchestrator and Developer roles in one session. A separate Review Agent holds the Reviewer role. The Designer works in an isolated session during planning and returns when the design must change.

```text
+--------+   discuss   +----------------------+
| Master | <---------> | Designer             |
+---+----+             | isolated session     |
    | accepts plan     +----------+-----------+
    +-----------------------------+
                                  v
                       +------------------------------+
                       | Build Agent                  |
                       | Orchestrator + Developer     |
                       | assign -> implement          |
                       +---------------+--------------+
                                       | submission
                                       v
                       +---------------+--------------+
                       | Review Agent                 |
                       | Reviewer                     |
                       | review -> integrate          |
                       +---------------+--------------+
                                       |
              +------------------------+-------------------------+
              | next Task ---------------------> Build Agent     |
              | contract or budget change -----> Designer        |
              | all Tasks done ----------------> Master accepts  |
              +--------------------------------------------------+
```

### 1. Install the Skill

Install [`skills/chassiss/`](skills/chassiss/) for every agent. Use the bundled CLI that matches the operating system and architecture.

### 2. Initialize the project and issue credentials

Create the Master Root and initialize the project.

```text
chassiss auth master-init
chassiss --credential ~/.chassiss/master-root.yaml \
  project init /path/to/project
```

Add `--existing` to `project init` when adopting an existing Git repository.

Enter the project and issue Designer, Build, Reviewer, and Owner credentials.

```text
cd /path/to/project

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor designer-1 --role designer \
  --output ~/.chassiss/cred-designer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role developer \
  --output ~/.chassiss/cred-build-developer.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor reviewer-1 --role reviewer \
  --output ~/.chassiss/cred-reviewer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor human-owner --role owner \
  --output ~/.chassiss/cred-human-owner.yaml
```

### 3. Start the Designer session

Export the Designer credential.

```text
chassiss auth export ~/.chassiss/cred-designer-1.yaml
```

Start an isolated session, tell the agent that it is the Designer, and provide the project path and the three-line Base64 armor. The Designer imports the credential, runs `bootstrap`, obtains the current templates, and prepares Requirements, Architecture, Mission, and Task artifacts. Master discusses the plan with the Designer and accepts the submitted artifacts.

### 4. Start the Build and Review agents

Export both Build credentials and the Reviewer credential.

```text
chassiss auth export ~/.chassiss/cred-build-orchestrator.yaml
chassiss auth export ~/.chassiss/cred-build-developer.yaml
chassiss auth export ~/.chassiss/cred-reviewer-1.yaml
```

Send both Build armors to the Build Agent and tell it to act as Orchestrator and Developer. Send the Reviewer armor to the independent Review Agent. Both agents import their credentials, run `bootstrap`, and follow the actions returned by the CLI.

### 5. Repeat the workflow

The Orchestrator assigns work, the Developer implements and submits it, and the Reviewer reviews and integrates it. Every agent refreshes `bootstrap` after a state change.

Return to the Designer when any of these conditions occurs.

- The current step budget or Task budget is exhausted
- Requirements or architecture changed
- A frozen Task contract needs a replacement

After Master accepts the next planning batch, the Build and Review agents continue. When all Tasks are complete, the Orchestrator submits Mission acceptance evidence and Master accepts the Mission.

The complete command and role documentation is available in [`docs/en/`](docs/en/).

> Users with a capable frontier agent can experiment with delegating the human Master role to that agent. It may issue credentials, create subagents, and manage the entire project. Root and role credentials will usually share one trust domain in this setup, so secret isolation is disabled by default. CHASSISS can still enforce one development workflow, but it does not guarantee project quality. This mode is outside the primary design target.
>
> If you run this experiment, successful and failed reports are both welcome in [GitHub Issues](https://github.com/ExplodeCode6324/chassiss/issues).

## Controlled project structure

```text
project-name/
├── .chassis/             # CLI-managed authorization, state, events, recovery
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   ├── missions/
│   └── tasks/
└── <source and ordinary project files>
```

Three boundaries must remain intact.

- Do not edit or delete `.chassis/`. The CLI also manages temporary cache according to active operations.
- Do not manually modify CHASSISS Git refs, branches, linked worktrees, or controlled Requirements, Architecture, Mission, and Task artifacts.
- Change ordinary project files only inside a CLI-created Task worktree or through the human Owner workflow.

## Independent human development

When a human needs to bypass the agent workflow, edit ordinary project files as Owner without creating a commit. Then run `owner apply --reason <reason>`. The CLI checks project state, creates the formal commit, and records signed audit evidence.

Owner cannot modify `.chassis/`, Git control data, or registered project artifacts. See [Human Owner takeover](docs/en/16-owner-takeover.md).

## Documentation

Choose a language from the [documentation home](docs/README.md), or open the [English documentation](docs/en/README.md) and its [chapter map](docs/en/menu.md) directly. An agent must still treat the trusted CLI `bootstrap` output as the authority for identity, permissions, context, and current actions.
