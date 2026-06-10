# Message of the Day (MOTD / NEWS)

Set the **MOTD File** system parameter (admin menu → System Parameters) to the
**absolute path** of a plain-text file. After a successful login, its contents
are shown in red, one page at a time, before the menu — press **ENTER** to page
through (PA3 and PF3 do nothing here). Leave the parameter empty to disable it.

The file is read **fresh on every login**, so edits take effect on the next
sign-on — no restart. Authoring rules:

- **Absolute path only.** A relative path is ignored (logged, then skipped).
- **Fixed width: lines truncate at column 79** ("80-column record length"). The
  file is a composed banner — you own the line breaks and indentation; anything
  past column 79 is dropped. Compose to fit.
- **Keep it short** — realistically no more than ~3 screens; longer notices tend
  to get paged past unread. Files larger than 8 KiB are clipped.
- Write maintenance windows in **UTC** by convention.
