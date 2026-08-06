# Drag and drop in f4

Dragging files between f4 and the rest of the desktop. The protocol side
lives in vtui (see its DRAGDROP.md); this file is the f4 side and the
roadmap.

Only graphical backends can do this. Terminals have no protocol for it, so
in a terminal nothing registers and nothing changes.

## What works now

The panels are a drop target. Files dropped on a panel land in the directory
under the cursor, or in the panel's current directory when the cursor is not
on a directory. Shift moves, Ctrl copies, otherwise the source's suggestion
is followed and copy is the fallback.

The destination is always the panel's own VFS, so a panel showing an archive
or a network connection is not a special case: the transfer is handed to
`ExecuteFileOp`, exactly like F5, with its progress, overwrite and error
dialogs, and its queue / background / foreground mode from the config. Files
coming from several source directories become several operations, run one
after another.

A file system that knows it is read-only can implement `IsReadOnly() bool`
and the pointer will say "no" before the drop instead of failing after it.

The other direction works too, under X11. Press the left button on a marked
file and move: the marked files are offered to the desktop as a file list.
A press on an unmarked file still only moves the cursor, so nothing about
the old mouse behaviour changed. Only copy is offered, and only from a local
panel - an archive or a network panel says so in a toast instead.

## Roadmap

1. done: backend-agnostic core in vtui, drop target in f4.
2. done: X11 XDND, receiving side. Dropping files from another application
   onto a panel works under X11. Two known limits: an INCR selection
   transfer is refused rather than half read, and move is only offered when
   the source publishes `XdndActionList`, since only the source can honour a
   move by deleting the original.
3. done: drag out under X11, from local panels, as copy.
4. Dragging out of an archive or a network panel. The files have to be
   materialised into a temporary directory first, which is a copy the user
   did not ask for: it needs a progress dialog, a cleanup on exit, and
   probably XDND's direct save (`XdndDirectSave0`) so nothing is copied
   until the receiver actually wants it.
5. Offering move as well as copy. Everything is in place except the trust:
   the source deletes the originals on the receiver's word, so this wants
   testing against real desktops before it is switched on.
</parameter>
4. Highlighting the drop target while the pointer is over it. Deliberately
   not done yet: it needs the panel to paint hover state, and until step 2
   lands there is nothing to hover with.
5. Wayland (`wl_data_device`), then gogpu, Windows (OLE) and macOS.
6. A protocol for terminals. None exists; if we invent one, far2l's
   extension channel is the natural place for it.

## Open questions, to review before step 3

- Dropping on ".." currently means the panel's directory, not its parent.
  Far itself has no drop target to copy here, and "the panel you see" is the
  less surprising of the two. Revisit if it annoys anyone.
- The destination is passed to `ExecuteFileOp` as a path in the target VFS.
  For archive VFSes whose paths are not absolute in the `IsAbs` sense this
  could take the wrong branch there; needs a test with a real archive panel
  once drops can actually arrive.
- A drop on a panel covered by info / quick view goes to that panel's
  directory. Alternatives are refusing it or dropping into the *other*
  panel; both looked worse than the obvious one.
- Move from another application deletes the source, and the source is
  outside f4. Step 2 should confirm that XDND's `XdndActionMove` really does
  mean "we deleted it" for the common senders before we offer move at all;
  if not, we should only ever announce copy.