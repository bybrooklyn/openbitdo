# AUR Publish SSH Troubleshooting

This runbook focuses on resolving AUR publish failures such as `Permission denied (publickey)` in release workflows.

## Preconditions
- `AUR_USERNAME` secret exists.
- `AUR_SSH_PRIVATE_KEY` secret exists and contains the full private key block.
- Runner can reach `aur.archlinux.org:22`.

## 1) Key format and permissions checks
Run on a secure local shell before updating secrets:

```bash
mkdir -p /tmp/aur-debug && cd /tmp/aur-debug
cat > aur_key <<'KEY'
<PASTE_PRIVATE_KEY>
KEY
chmod 600 aur_key
ssh-keygen -y -f aur_key >/tmp/aur_key.pub
```

Expected:
- `ssh-keygen -y` succeeds.
- no passphrase prompt for CI use.

## 2) Known hosts and host verification

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
ssh-keyscan -H aur.archlinux.org >> ~/.ssh/known_hosts
chmod 600 ~/.ssh/known_hosts
```

Expected:
- `aur.archlinux.org` host key is present in `known_hosts`.

## 3) SSH dry-run authentication

```bash
ssh -i /tmp/aur-debug/aur_key \
  -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes \
  ${AUR_USERNAME}@aur.archlinux.org
```

Expected success signature:
- authentication accepted (AUR may close session after auth; that still proves key acceptance).

Expected failure signatures:
- `Permission denied (publickey)` means wrong key/user pairing.
- `Host key verification failed` means known_hosts mismatch/missing.

## 4) Repo-level publish dry run
For package repo:

```bash
git ls-remote ssh://${AUR_USERNAME}@aur.archlinux.org/openbitdo-bin.git
```

Expected:
- command returns refs without auth failures.

## 5) CI secret update checklist
- Store private key in `AUR_SSH_PRIVATE_KEY` exactly as multiline PEM/OpenSSH block.
- Store account name in `AUR_USERNAME`.
- Re-run release workflow preflight job.

## 6) Post-fix validation
- Confirm release preflight no longer fails on SSH auth.
- Confirm `publish-aur` job pushes `openbitdo-bin` metadata repo.
