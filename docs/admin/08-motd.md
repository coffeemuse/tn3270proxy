# Documents: MOTD and Login Branding

The post-login **MOTD/NEWS** text and the **login-screen branding** art are
stored in the database and managed from the admin UI: admin menu →
**8 Documents**. The screen is a member list (think ISPF PDS member list):

    Name        Lines  Changed (UTC)      ID
    MOTD           12  2026-06-11 14:02   ADMIN
    BRANDING       18  2026-06-10 09:30   MIGRATION

Line commands: **E** = edit in place, **I** = import from a server file.
Changes take effect on the next render — the MOTD on the next login, branding
on the next login-screen paint. No restart. An **empty document disables the
feature** (no MOTD gate; blank branding region).

## The editor (E)

An ISPF-style line editor: type over text, and use the 2-character prefix area
at the left of each line for structural commands —

- **I** — insert a blank line after this one
- **D** — delete this line
- **R** — repeat (duplicate) this line

Press **Enter** to apply, **PF3** to save and return, **PF12** to cancel
(discard all changes), **PF7/PF8** to page. Within a line, your emulator's
**Insert** key works natively.

Two width rules, both inherited from the 3270 screen:

- Rendered output truncates at **column 79** (the documents are composed
  fixed-width banners — you own line breaks and indentation).
- The editor itself can edit lines up to **76 characters** (the prefix area
  costs 4 columns). Wider lines show in **yellow, protected**: you can still
  delete them or insert around them, but changing their content requires
  re-import. The editor never truncates your art.

Documents are capped at **8 KiB**; the editor refuses to save past the cap.

## Import (I)

For full-width or complex ASCII art, author with your favorite editor and
import. The import screen asks for an **absolute server-side path** — pre-filled
from the matching system parameter (**MOTD Import Path** / **Branding Import
Path**, admin menu → Sysparms), so the usual flow is: copy the file to the
configured path (`scp`, a volume mount, …) and press Enter. Import replaces the
document's entire content. Files over 8 KiB are rejected.

The CLI sibling works without a 3270 session and can also round-trip content
back out for offline editing:

    tn3270proxy doc import -db proxy.db -name BRANDING -file art.txt
    tn3270proxy doc export -db proxy.db -name BRANDING -file art.txt

## Authoring conventions

- Fixed width: compose to **80 columns or less**; anything past column 79 is
  not rendered.
- Keep the MOTD short — realistically no more than ~3 screens; longer notices
  get paged past unread. Press ENTER to page; PA3/PF3 do nothing on the MOTD.
- Write maintenance windows in **UTC** by convention.
- Branding lines render verbatim from column 0 on the login screen body;
  vertical centering is automatic when the art is shorter than the body.
- Editing a line in the 3270 editor strips its trailing spaces (a 3270 terminal
  cannot distinguish trailing blanks from empty positions); art that depends on
  trailing spaces survives import and render, but not an in-editor edit of that
  line.

Saves and imports are audited (`doc_update` / `doc_import`) with the actor,
line count, and (for imports) the source path.

## HELP-MENU — service menu help text

A third document, **HELP-MENU**, holds the text displayed when a user presses
**PF1** at the service menu. Unlike MOTD and BRANDING (which seed empty), this
document ships with **stock content** so PF1 works immediately on a fresh
install without any admin action.

The Documents member list (option 8) shows it alongside MOTD and BRANDING:

    Name        Lines  Changed (UTC)      ID
    HELP-MENU      26                     

Edit and import work identically to the other documents — **E** opens the
ISPF line editor, **I** opens the import form. Note that the import form's
path field starts **blank** for HELP-MENU (there is no system parameter for a
default import path — unlike MOTD and BRANDING, HELP-MENU has no matching
sysconfig import-path entry).

The CLI verbs work the same way:

    tn3270proxy doc export -db proxy.db -name HELP-MENU -file help-menu.txt
    tn3270proxy doc import -db proxy.db -name HELP-MENU -file help-menu.txt

**Blanking the document disables help**: if the content is cleared to empty,
pressing PF1 shows `NO HELP AVAILABLE` on the menu's message line instead of
opening a viewer. This is intentional — operators can suppress help by
clearing the document. Edits, including blanking, survive restarts and
upgrades (the stock seed uses INSERT OR IGNORE, so it only runs once).
