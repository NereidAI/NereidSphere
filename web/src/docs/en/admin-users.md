<!-- Copyright (C) 2026 YIQISOFT; SPDX-License-Identifier: Apache-2.0 -->

# Account Management

The **Accounts** page (user menu → Accounts) manages suite accounts from
NereidSphere. It is available in OIDC mode only and requires a suite role:

- **Administrator** — view and manage accounts;
- **Operator** — view accounts (read-only);
- **User** — no access to the account console.

Administrators never sign in to the identity engine's own administration
console. NereidSphere uses the `nereidsphere-admin` service account for the
approved account lifecycle operations only.

## Search and pagination

Use the search box to find accounts by username, display name or email. The
identity service performs the search and returns one server-side page at a
time. Changing the page size or page number queries the identity service
again; results are not limited to the browser's currently loaded rows.

## Account operations

Administrators can:

- create a local account with username, optional email, display name, initial
  password and suite roles;
- edit email and display name;
- enable or disable an account;
- reset a password;
- assign `User`, `Operator` or `Administrator` roles;
- permanently delete an account.

By default, new and reset passwords are marked temporary. The user must change
the password on the next sign-in. Disable an account when it may have audit
history or ownership of business data; use permanent deletion only when the
account should be purged.

Role changes take effect on the user's next token refresh. Disabling an
account prevents new sign-ins.

## Product tenant and roles

In OIDC mode the create and edit dialogs also assign a tenant and role per
suite product. Each assignment is stored as an identity-service group named
`/nereid{product}/{tenant}/{role}` (for example
`/nereidlink/tenant-a/admins`), so every product can map group to role with
its own configuration. Products that do not follow the convention are absent
from the picker.

The tenant list is loaded at runtime from each product through the gateway, so
tenants stay owned by the product; you can also type a tenant id directly if a
product does not expose a list. Assignment is additive and idempotent: only the
groups that differ from the current state are added or removed. External
accounts remain read-only.

## Safety rules

NereidSphere prevents administrator lockout:

- an administrator cannot delete or disable their own account;
- an administrator cannot remove the administrator role from their own
  account;
- the last enabled administrator cannot be disabled, deleted or demoted.

Destructive and high-impact operations require confirmation in the UI. Account
deletion additionally requires typing the account username.

## Email status

The email column shows whether the identity service marks the address as
verified. NereidSphere does not guess verification status from the address
format. External accounts display the verification state supplied by their
identity provider.

## External accounts are read-only

Accounts provisioned by an external identity provider are tagged with the
provider alias and shown as **External account**. Their password, enabled
state, profile and roles must be managed in that provider. NereidSphere
rejects mutation attempts with `FEDERATED_READ_ONLY`.

See [Connection status](help:page/connection-status) and
[Identity provider](help:glossary/identity-provider).
