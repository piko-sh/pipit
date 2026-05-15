---
title: "Verify a release"
description: "Verify archive signatures and software bills of materials."
section: "How-to guides"
order: 5
---

# Verify a release

The [release workflow](../.github/workflows/release.yml) is configured to publish
CLI archives, detached signatures (`.sig`), signing certificates (`.cert`), and
CycloneDX/SPDX software bills of materials (SBOMs), which list packaged components. Verify the files attached to your
selected release before using them. You need its signatures and certificates
to complete these steps.

## Verify an archive

Download the archive and its matching `.sig` and `.cert` files from the
[Pipit releases page](https://github.com/piko-sh/pipit/releases). Install Cosign
with support for detached signature/certificate verification, such as Cosign 2.x.
The [Cosign verify-blob reference](https://github.com/sigstore/cosign/blob/v2.4.1/doc/cosign_verify-blob.md)
describes these flags.

Set `tag` to the release tag you selected and `archive` to the downloaded filename.
Windows archives use `.zip` instead of `.tar.gz`.
For a release built from a tag, verify the exact workflow identity and GitHub issuer:

```sh
tag='vYOUR_RELEASE'
archive='pipit-YOUR_ARCHIVE.tar.gz'
cosign verify-blob "$archive" \
  --signature "$archive.sig" \
  --certificate "$archive.cert" \
  --certificate-identity "https://github.com/piko-sh/pipit/.github/workflows/release.yml@refs/tags/$tag" \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Successful verification checks the archive against the signature and the selected
signer identity. Keep the identity exact; do not replace it with an unrestricted
regular expression. The detached format may need online access to the transparency
log. Stop if a signature or certificate is missing, or verification fails.

The release also includes `checksums-sha256.txt`, and each archive contains a
`SHA256SUMS` file. These checksums are not signed. They can detect a damaged
download, but only the signature confirms where an archive came from.

## Verify an SBOM

Each archive has `<archive>.sbom.cdx.json` and `<archive>.sbom.spdx.json` assets,
with their own `.sig` and `.cert` files. Use the same command with `archive` set
to the SBOM filename. The SBOM describes packaged components; a valid signature
does not establish that those components are vulnerability-free.

The workflow also requests GitHub build-provenance attestations for archives.
These are separate from the detached signatures described above.
