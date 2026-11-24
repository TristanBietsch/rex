# Release signing

Status: not started

## Problem

`rex update` and the installers check `SHA256SUMS` from the same release as
the archive. Whoever controls the GitHub account or the release workflow can
replace both, so the check proves only that the download matches what GitHub
published. macOS binaries are not signed.

A signature made from what CI built would not help: someone who controls the
account or CI controls what gets built, and the maintainer would sign it. A
signature must come from a build the maintainer has checked on their own
machine.

Already done: the release workflow refuses a tag whose commit is not on
`master`, and `SECURITY.md` and `README.md` say what a release check proves.

## To do

- Rebuild each release from the tag on the maintainer's machine and compare
  hashes with the CI build before signing. Linux builds use cgo, so this
  needs a pinned toolchain or a container.
- Sign in SSHSIG format (`ssh-keygen -Y sign`, checked with `ssh-keygen -Y
  verify`) rather than raw Ed25519. Every supported OS ships `ssh-keygen`;
  the LibreSSL `openssl` on macOS cannot verify raw Ed25519 signatures.
- Make `rex update`, `install.sh` and `install.ps1` verify the signature.
- Sign and notarize macOS binaries with an Apple Developer ID.
- Set GitHub tag-protection rules for `v*` tags.

## Open questions

- Where does the public key live, and how does a client learn of a key
  change? Pinned in the binary, in the repository, or both?
- How is the signing key stored and backed up, and who can use it?
- Which pinned toolchain or container reproduces the Linux cgo builds bit for
  bit? Are macOS and Windows builds reproducible at all?
- What does `rex update` do when the signature is missing on an older release?
- Is a transparency log worth adding on top?
