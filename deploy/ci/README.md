# Image pipeline

The workflow tests the Go code with race detection, builds a backend image without migrations, and checks/tests/builds the pinned frontend source. Official base images and Actions are pinned by digest/commit. Production credentials are never build inputs.

Trusted pushes to master or codex/k3s-launch publish commit-tagged images to ghcr.io/traliaa/trade-bot-backend and ghcr.io/traliaa/trade-bot-frontend. Pull requests test/build without publishing. A separate job verifies anonymous pulls and proposes an image-only GitOps PR into the triggering branch; repository settings must allow GitHub Actions to create PRs. The PR is never automatically merged or deployed.

New GHCR packages may default to private. Both packages must permit anonymous pulls before promotion; the workflow fails safely otherwise. Existing workloads keep running until a reviewed manifest update is synced. GITHUB_TOKEN is ephemeral and scoped separately for package publishing and the GitOps proposal.

Backend OnDelete remains deliberate. After syncing an approved new image, perform a controlled stop, confirm the old Pod/process has exited, and start the replacement. Never force-delete or move a partitioned backend. The frontend rolls normally. Automated cross-site failover remains a separate task.

The frontend repository/revision lives in frontend-source.json. To rebuild a frontend change, update that pinned revision through review. Avoid using a floating branch as a build source.
