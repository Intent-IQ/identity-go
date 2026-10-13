# Releasing

1. In a pull request, update `version.env` and make every `integrations/*/go.mod`
   require that root version. Merge it and wait for `main` CI to pass.
2. Run the **Release** workflow from the GitHub Actions page. It validates the
   repository, pushes all module tags, and creates the root GitHub release with
   generated release notes.

Pushing a tag publishes that Go module. Never move or reuse a published tag.
