# AUR Publish Troubleshooting

Use this runbook when the AUR release path fails, especially on SSH authentication.

## Typical Failure Signatures

- `Permission denied (publickey)`
- `Host key verification failed`
- missing `AUR_USERNAME`
- missing `AUR_SSH_PRIVATE_KEY`

## Local SSH Sanity Check

```bash
mkdir -p /tmp/aur-debug && cd /tmp/aur-debug
cat > aur_key <<'KEY'
<PASTE_PRIVATE_KEY>
KEY
chmod 600 aur_key
ssh-keygen -y -f aur_key >/tmp/aur_key.pub
```

Expected:

- the key parses successfully
- no CI-incompatible passphrase prompt

## Known Hosts Check

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
ssh-keyscan -H aur.archlinux.org >> ~/.ssh/known_hosts
chmod 600 ~/.ssh/known_hosts
```

## Remote Auth Check

```bash
ssh -i /tmp/aur-debug/aur_key \
  -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes \
  "${AUR_USERNAME}@aur.archlinux.org"
```

An immediate disconnect after auth is still acceptable. It proves the key is valid.

## Repo Check

```bash
git ls-remote "ssh://${AUR_USERNAME}@aur.archlinux.org/openbitdo-bin.git"
```

If this fails, the AUR account or key pairing is still wrong.

## After The Fix

- rerun the release workflow
- confirm `publish-aur` succeeds
- confirm `openbitdo-bin` points at the new release tag
