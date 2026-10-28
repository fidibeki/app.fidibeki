# Open-source release checklist

Use this checklist when publishing a release from this repository.

1. Choose and add the repository license. A public release must not proceed without it.
2. Create a new public repository from the sanitized source tree. Do not change the visibility of the existing private repository, because its Git history contains removed internal material.
3. Copy only tracked files from the reviewed release commit. Do not copy ignored files, local environment files, build outputs, test results, or untracked plans.
4. Configure the deployed legal notices with the actual operator identity, address, jurisdiction, and contact details. The marketing legal pages are templates.
5. Configure monitored support and security addresses before publishing `SECURITY.md` and the marketing site.
6. Publish a visible source-code link for users of every hosted deployment. AGPL-3.0 section 13 requires network users to receive the corresponding source.
7. Run the project's tests, type checks, builds, and secret scan against the exact release tree.
8. Review the first public commit and repository settings, including branch protection, private vulnerability reporting, and CI secrets.
