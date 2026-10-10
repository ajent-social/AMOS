# Personal workspace bootstrap in the native writer

`NewWriter` binds bootstrap to the caller's original attempt. Pending bootstrap
requires a live same-attempt PendingRegistration, an acquired reserved person,
and an acquired reserved workspace. It creates an active personal workspace
while the person remains pending verification. Both records share the caller's
transaction; this participant cannot commit or activate a person/session.

Active bootstrap still requires the native root to obtain its principal from
private session recheck and apply workspace policy. A public Principal argument
is not proof of current authority. Existing-workspace lookup checks the returned
ID against the acquired inventory before disclosure. New persistence reuses the
workspace store participant and its terminal failure marker.

Required-service checks exercise actual registration/workspace atomicity with
an explicitly synthetic encoded verifier. They do not qualify password hashing,
email delivery, current-session composition or an application host.
