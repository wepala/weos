# WeOS CMS — Permission System
## EARS Requirements Specification · v1.0

---

## 1. Document Overview

This document specifies the functional requirements for the WeOS CMS permission system using the Easy Approach to Requirements Syntax (EARS). Requirements are organized by functional area and use standard EARS patterns to ensure precision and testability.

### 1.1 Authorization Model

The WeOS permission system implements a **Hybrid RBAC + Ownership** model:

- **Role-Based Access Control (RBAC)** governs the majority of permission decisions through structured role assignments.
- **Ownership rules** provide relationship-based overrides, allowing content creators to retain edit access to their own drafts regardless of role.
- **Conflict resolution** follows a most-restrictive-wins policy: an explicit deny on any active role overrides all grants.

### 1.2 EARS Pattern Legend

| Pattern | Syntax |
|---|---|
| **Ubiquitous** | Applies always — no trigger or condition |
| **Event-driven** | WHEN [trigger] the system shall… |
| **State-driven** | WHILE [state] the system shall… |
| **Unwanted Behavior** | IF [condition] the system shall… |
| **Optional Feature** | WHERE [feature is included] the system shall… |

---

## 2. Roles & Role Hierarchy

### 2.1 Built-in Roles

The system ships with five built-in roles that cannot be deleted.

| ID | Pattern | Requirement |
|---|---|---|
| ROLE-01 | Ubiquitous | The system shall provide a built-in **Super Admin** role that grants unrestricted access to all resources and administrative functions across all sites. |
| ROLE-02 | Ubiquitous | The system shall provide a built-in **Site Admin** role that grants full administrative access scoped to one or more specific sites. |
| ROLE-03 | Ubiquitous | The system shall provide a built-in **Editor** role that grants permission to create, read, update, delete, publish, unpublish, and archive any content within the assigned scope. |
| ROLE-04 | Ubiquitous | The system shall provide a built-in **Author** role that grants permission to create content and read, update, and delete only content owned by that user within the assigned scope. |
| ROLE-05 | Ubiquitous | The system shall provide a built-in **Viewer** role that grants read-only access to content within the assigned scope. |
| ROLE-06 | Ubiquitous | The system shall prevent deletion or modification of any built-in role definition. |

### 2.2 Role Inheritance

| ID | Pattern | Requirement |
|---|---|---|
| ROLE-07 | Ubiquitous | The system shall implement role inheritance such that each role implicitly includes all permissions of the role(s) below it in the hierarchy: `Super Admin > Site Admin > Editor > Author > Viewer`. |
| ROLE-08 | Unwanted Behavior | IF a role definition introduces a circular inheritance chain, the system shall reject the configuration and return a validation error. |

### 2.3 Role Assignment

| ID | Pattern | Requirement |
|---|---|---|
| ROLE-09 | Ubiquitous | The system shall allow a user to hold multiple roles simultaneously, each potentially with a different scope. |
| ROLE-10 | Ubiquitous | The system shall support scoping a role assignment to one of: (a) the entire system (global), (b) a specific site, (c) a specific content type, or (d) a specific section or folder. |
| ROLE-11 | Event-driven | WHEN a user is assigned a role at a given scope, the system shall apply that role's permissions only to resources that fall within that scope. |
| ROLE-12 | Event-driven | WHEN a user has roles assigned at multiple scopes, the system shall evaluate all applicable role assignments and apply the most restrictive result. |

---

## 3. Content Permissions

### 3.1 Core CRUD Actions

| ID | Pattern | Requirement |
|---|---|---|
| CONT-01 | Event-driven | WHEN a user requests to create content, the system shall permit the action only if the user holds a role of Author or higher within the target scope. |
| CONT-02 | Event-driven | WHEN a user requests to read content, the system shall permit the action only if the user holds a role of Viewer or higher within the target scope. |
| CONT-03 | Event-driven | WHEN a user requests to update content they do not own, the system shall permit the action only if the user holds a role of Editor or higher within the target scope. |
| CONT-04 | Event-driven | WHEN a user requests to delete content they do not own, the system shall permit the action only if the user holds a role of Editor or higher within the target scope. |
| CONT-05 | Unwanted Behavior | IF a user requests to read, update, or delete content outside all of their assigned scopes, the system shall deny the request and return an authorization error. |

### 3.2 Lifecycle Actions (Publish, Unpublish, Archive)

| ID | Pattern | Requirement |
|---|---|---|
| CONT-06 | Event-driven | WHEN a user requests to publish or unpublish content, the system shall permit the action only if the user holds a role of Editor or higher within the target scope. |
| CONT-07 | Event-driven | WHEN a user requests to archive content, the system shall permit the action only if the user holds a role of Editor or higher within the target scope. |
| CONT-08 | Unwanted Behavior | IF a user with the Author role attempts to publish their own content, the system shall deny the request and present a message indicating that publish requires Editor-level access or above. |

### 3.3 Permission Delegation

| ID | Pattern | Requirement |
|---|---|---|
| CONT-09 | Event-driven | WHEN a user requests to assign permissions on a content resource to another user, the system shall permit the action only if the requesting user holds the Assign Permissions right on that resource within the target scope. |
| CONT-10 | Unwanted Behavior | IF a user attempts to delegate a permission they do not themselves hold, the system shall deny the delegation request and return an authorization error. |
| CONT-11 | Ubiquitous | The system shall prevent any user from escalating their own permissions through delegation. |

---

## 4. Ownership & Relationship Rules

### 4.1 Author Ownership

| ID | Pattern | Requirement |
|---|---|---|
| OWN-01 | State-driven | WHILE a content item is in draft state, the system shall permit its owning author to read and update it, regardless of any other role assignment. |
| OWN-02 | Unwanted Behavior | IF a content item's owner attempts to publish it directly, the system shall deny the action and indicate that publishing requires Editor-level access or above, even if the author invokes their ownership rights. |
| OWN-03 | Unwanted Behavior | IF a user who is not the content owner and does not hold Editor or higher within the target scope attempts to update that content, the system shall deny the request. |

### 4.2 User Deletion & Content Reassignment

| ID | Pattern | Requirement |
|---|---|---|
| OWN-04 | Event-driven | WHEN a user account is deleted, the system shall automatically reassign all content owned by that user to the Site Admin of the site in which each content item resides. |
| OWN-05 | Event-driven | WHEN ownership of a content item is reassigned, the system shall update the owning user reference and apply the new owner's permissions from that point forward. |
| OWN-06 | Unwanted Behavior | IF no Site Admin exists for a site at the time of user deletion, the system shall assign orphaned content ownership to the Super Admin and generate an alert. |

---

## 5. Scope Evaluation

| ID | Pattern | Requirement |
|---|---|---|
| SCOPE-01 | Ubiquitous | The system shall evaluate permissions by first checking for a global role assignment, then site-scoped, then content-type-scoped, then section/folder-scoped, applying the most specific scope where a role is found. |
| SCOPE-02 | Ubiquitous | The system shall apply a most-restrictive-wins conflict resolution policy: if any active role assignment produces an explicit deny for a requested action, the system shall deny the action regardless of grants from other roles. |
| SCOPE-03 | Unwanted Behavior | IF no role assignment grants the requested permission at any applicable scope and no ownership rule applies, the system shall default to deny. |
| SCOPE-04 | Event-driven | WHEN a scope (site, content type, or section) is deleted, the system shall revoke all role assignments that were bound exclusively to that scope. |

---

## 6. User Management Permissions

| ID | Pattern | Requirement |
|---|---|---|
| USER-01 | Event-driven | WHEN a Site Admin requests to assign or revoke a role for a user, the system shall permit the action only if the target user and the role assignment fall within that Site Admin's site scope. |
| USER-02 | Unwanted Behavior | IF a Site Admin attempts to grant the Super Admin role or a role assignment that exceeds their own site scope, the system shall deny the action and return an authorization error. |
| USER-03 | Unwanted Behavior | IF a user who is not a Site Admin or Super Admin attempts to assign or revoke roles, the system shall deny the action. |
| USER-04 | Ubiquitous | The system shall allow only the Super Admin to create, modify, or delete Site Admin role assignments. |

---

## 7. Audit Logging

| ID | Pattern | Requirement |
|---|---|---|
| AUDIT-01 | Event-driven | WHEN a role is granted to a user, the system shall write an immutable audit log entry recording: the granting user, the target user, the role granted, the scope, and the timestamp. |
| AUDIT-02 | Event-driven | WHEN a role is revoked from a user, the system shall write an immutable audit log entry recording: the revoking user, the target user, the role revoked, the scope, and the timestamp. |
| AUDIT-03 | Ubiquitous | The system shall make audit logs for all permission changes visible to the Super Admin across all sites. |
| AUDIT-04 | Ubiquitous | The system shall make audit logs for permission changes within a site visible to the Site Admin of that site. |
| AUDIT-05 | Ubiquitous | The system shall prevent modification or deletion of audit log entries by any role, including Super Admin. |

---

## 8. Enforcement & System Behaviour

| ID | Pattern | Requirement |
|---|---|---|
| ENF-01 | Ubiquitous | The system shall enforce authorization checks server-side on every API request, regardless of client-side permission state. |
| ENF-02 | Event-driven | WHEN a permission check is denied, the system shall return a standardized authorization error response without revealing the existence or contents of resources the requester has no access to. |
| ENF-03 | State-driven | WHILE a user session is active, the system shall re-evaluate permissions on each request using the current state of role assignments, not a cached state from session initiation. |
| ENF-04 | Event-driven | WHEN a user's role assignment changes, the system shall invalidate any cached permission state for that user immediately. |
| ENF-05 | Unwanted Behavior | IF a request arrives without valid authentication credentials, the system shall deny all actions and return an authentication error before any authorization check is performed. |

---

## Appendix A: Requirement ID Reference

| Prefix | Functional Area |
|---|---|
| `ROLE-` | Role definitions, hierarchy, and assignment |
| `CONT-` | Content permissions and lifecycle actions |
| `OWN-` | Ownership and relationship-based rules |
| `SCOPE-` | Scope evaluation and conflict resolution |
| `USER-` | User management permission controls |
| `AUDIT-` | Audit logging requirements |
| `ENF-` | System-wide enforcement rules |