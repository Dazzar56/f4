package fishplus

import (
	"context"
	"strconv"
)

// MkDir creates a directory and any missing parent of it, so a caller does
// not pay a round trip per level.
func (c *Client) MkDir(ctx context.Context, p string) error {
	resp, err := c.sess.ExecPath(ctx, "mkdir", p)
	if err != nil {
		return err
	}
	return resp.Err("mkdir " + p)
}

// Remove deletes a file or a symlink. A path that is not there is not an
// error, which matches what the panel expects when two operations race.
func (c *Client) Remove(ctx context.Context, p string) error {
	resp, err := c.sess.ExecPath(ctx, "rm", p)
	if err != nil {
		return err
	}
	return resp.Err("rm " + p)
}

// RemoveDir deletes an empty directory.
func (c *Client) RemoveDir(ctx context.Context, p string) error {
	resp, err := c.sess.ExecPath(ctx, "rmdir", p)
	if err != nil {
		return err
	}
	return resp.Err("rmdir " + p)
}

// RemoveAll deletes a directory with everything below it. The remote host
// does the walking, which is the whole point of a shell based file system:
// the classic alternative is one round trip per entry. The helper refuses to
// aim this at the root directory.
func (c *Client) RemoveAll(ctx context.Context, p string) error {
	resp, err := c.sess.ExecPath(ctx, "rmtree", p)
	if err != nil {
		return err
	}
	return resp.Err("rmtree " + p)
}

// Rename moves a path, overwriting the destination the way mv does.
func (c *Client) Rename(ctx context.Context, from, to string) error {
	resp, err := c.sess.ExecPaths(ctx, "mv", []string{from, to})
	if err != nil {
		return err
	}
	return resp.Err("mv " + from)
}

// Chmod sets the permission, setuid, setgid and sticky bits; the file type
// bits of a raw st_mode are ignored, so an Entry.Mode can be passed as is.
func (c *Client) Chmod(ctx context.Context, p string, mode uint32) error {
	octal := strconv.FormatUint(uint64(mode&07777), 8)
	resp, err := c.sess.ExecPath(ctx, "chmod", p, octal)
	if err != nil {
		return err
	}
	return resp.Err("chmod " + p)
}