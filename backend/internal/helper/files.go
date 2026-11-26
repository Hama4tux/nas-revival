package helper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/shares"
)

// File Station (W4.1, FS-01..03, ADR 0009). The web process never opens the
// shares itself: every operation names a share and a path relative to it,
// the helper checks the caller's access with the same rules as SMB
// (shares.Effective) and resolves the path through os.Root, so ".." or a
// symlink can never lead out of the share folder. New entries belong to the
// logged-in user and group nas-users, like files created over SMB.

// Error codes of the File Station.
const (
	CodeFileExists = "file_exists"
	CodeForbidden  = "forbidden"
)

const (
	// MaxFileItems is how many entries one move or delete takes.
	MaxFileItems = 100
	// maxRecycleTouch bounds the files touched when a folder goes to the bin.
	maxRecycleTouch = 200000
	maxNameBytes    = 255
	recycleDirName  = ".recycle"
	// uploadPrefix marks unfinished uploads; they are hidden from listings.
	uploadPrefix = ".nas-upload-"
	// uploadReserve is kept free on the volume besides the upload itself.
	uploadReserve = 64 << 20
)

var uploadTokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// FileManager runs the File Station operations.
type FileManager struct {
	Config func() (shares.Config, error)
	DB     func() (accounts.DB, error)
	// HasACL says whether the volume enforces POSIX ACLs; without them the
	// helper gives new entries the modes Samba uses (0660/0770).
	HasACL func(volume string) bool
	// Chown sets the owner of a new entry (os.File.Chown; tests record it).
	Chown func(f *os.File, uid, gid int) error
	// Statfs reports the free bytes of the filesystem of f.
	Statfs func(f *os.File) (uint64, error)
	Now    func() time.Time
}

// FileShare is a share the caller may open.
type FileShare struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Access      string `json:"access"` // rw | ro
	Recycle     bool   `json:"recycle"`
}

// FileShareList is the result of fs.shares.
type FileShareList struct {
	Shares []FileShare `json:"shares"`
	// NoAccount is true when the caller is not a NAS account (the recovery
	// administrator), which has no files of its own.
	NoAccount bool `json:"noAccount"`
}

// FileList is the result of fs.list.
type FileList struct {
	BrowseResult
	Share  string `json:"share"`
	Access string `json:"access"`
}

// FileItemError is one entry a move or delete could not process.
type FileItemError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// FileBatch is the result of fs.move and fs.delete.
type FileBatch struct {
	Done     int             `json:"done"`
	Failed   []FileItemError `json:"failed"`
	Recycled bool            `json:"recycled"`
}

// UploadTicket is the JSON part of fs.uploadOpen; the descriptor of the
// temporary file travels next to it.
type UploadTicket struct {
	Token string `json:"token"`
}

// fileCtx is a resolved share of one caller.
type fileCtx struct {
	share  shares.Share
	access string
	uid    int
	gid    int
	user   string
	acl    bool
	root   *os.Root
}

func (c *fileCtx) close() { c.root.Close() }

func forbidden(msg string) *Error { return &Error{Code: CodeForbidden, Message: msg} }

func existsErr(msg string) *Error { return &Error{Code: CodeFileExists, Message: msg} }

// accessOf is the caller's access to s, the same matrix the SMB side gets.
func accessOf(s shares.Share, db accounts.DB, user string) string {
	for _, ua := range shares.Effective(s, db) {
		if ua.User == user {
			return ua.Access
		}
	}
	return shares.AccessNone
}

// Shares lists the shares the caller may open, by name.
func (m *FileManager) Shares(ctx context.Context, user string) (FileShareList, error) {
	db, err := m.DB()
	if err != nil {
		return FileShareList{}, err
	}
	out := FileShareList{Shares: []FileShare{}}
	if !db.IsNASUser(user) {
		out.NoAccount = true
		return out, nil
	}
	cfg, err := m.Config()
	if err != nil {
		return FileShareList{}, err
	}
	for _, s := range cfg.Shares {
		if a := accessOf(s, db, user); a != shares.AccessNone {
			out.Shares = append(out.Shares, FileShare{Name: s.Name, Description: s.Description, Access: a, Recycle: s.Recycle.Enabled})
		}
	}
	return out, nil
}

// open resolves share for user; write requires rw access.
func (m *FileManager) open(user, share string, write bool) (*fileCtx, error) {
	db, err := m.DB()
	if err != nil {
		return nil, err
	}
	u, ok := db.User(user)
	g, gok := db.Group(accounts.UsersGroup)
	if !ok || !gok || !db.IsNASUser(user) {
		return nil, forbidden("Soubory má jen účet NASu; záchranný správce je nemá.")
	}
	cfg, err := m.Config()
	if err != nil {
		return nil, err
	}
	for _, s := range cfg.Shares {
		if s.Name != share {
			continue
		}
		a := accessOf(s, db, user)
		if a == shares.AccessNone {
			break
		}
		if write && a != shares.AccessRW {
			return nil, forbidden("Do této sdílené složky máte jen čtení.")
		}
		if !realDir(s.Path()) {
			return nil, notFound("Složka sdílení na disku chybí (svazek není připojený?).")
		}
		r, err := os.OpenRoot(s.Path())
		if err != nil {
			return nil, notFound("Složka sdílení na disku chybí (svazek není připojený?).")
		}
		return &fileCtx{share: s, access: a, uid: u.UID, gid: g.GID, user: user, acl: m.HasACL(s.Volume), root: r}, nil
	}
	// Unknown and forbidden shares look the same.
	return nil, notFound("Sdílená složka neexistuje nebo k ní nemáte přístup.")
}

// validFilePath is a version path that also keeps out of the recycle bin and
// unfinished uploads.
func validFilePath(p string) bool {
	if !ValidVersionPath(p) {
		return false
	}
	if p == "" {
		return true
	}
	segs := strings.Split(p, "/")
	if segs[0] == recycleDirName {
		return false
	}
	for _, s := range segs {
		if strings.HasPrefix(s, uploadPrefix) {
			return false
		}
	}
	return true
}

// ValidFileName checks a new name: one element, UTF-8, at most 255 bytes, no
// characters Windows clients cannot show, no leading or trailing space and no
// trailing dot.
func ValidFileName(n string) bool {
	if n == "" || n == "." || n == ".." || len(n) > maxNameBytes || !utf8.ValidString(n) {
		return false
	}
	if strings.TrimSpace(n) != n || strings.HasSuffix(n, ".") || strings.HasPrefix(n, uploadPrefix) || n == recycleDirName {
		return false
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return false
		}
	}
	return true
}

func checkPath(p string) error {
	if !validFilePath(p) {
		return invalid("path", "Neplatná cesta.")
	}
	return nil
}

func checkName(n string) error {
	if !ValidFileName(n) {
		return invalid("name", `Neplatný název (bez znaků / \ : * ? " < > |, bez mezery na začátku či konci a tečky na konci).`)
	}
	return nil
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func parent(p string) string {
	if d := path.Dir(p); d != "." {
		return d
	}
	return ""
}

func hideInShare(dir string) func(string) bool {
	return func(n string) bool {
		return strings.HasPrefix(n, uploadPrefix) || (dir == "" && n == recycleDirName)
	}
}

// List lists one directory of a share (FS-01).
func (m *FileManager) List(ctx context.Context, user, share, p string, offset int) (FileList, error) {
	if err := checkPath(p); err != nil {
		return FileList{}, err
	}
	if offset < 0 {
		return FileList{}, invalid("offset", "Neplatný posun.")
	}
	c, err := m.open(user, share, false)
	if err != nil {
		return FileList{}, err
	}
	defer c.close()
	res, err := listDir(ctx, c.root, p, offset, hideInShare(p))
	if err != nil {
		return FileList{}, err
	}
	return FileList{BrowseResult: res, Share: share, Access: c.access}, nil
}

// Open opens a regular file for download (FS-03).
func (m *FileManager) Open(ctx context.Context, user, share, p string) (*fileResult, error) {
	if p == "" {
		return nil, invalid("path", "Neplatná cesta.")
	}
	if err := checkPath(p); err != nil {
		return nil, err
	}
	c, err := m.open(user, share, false)
	if err != nil {
		return nil, err
	}
	defer c.close()
	fr, err := openRegular(c.root, p)
	if err == nil {
		slog.Info("helper: fs.download", "share", share, "user", user)
	}
	return fr, err
}

// own gives a new entry the caller as owner, group nas-users and, without
// ACLs, the Samba modes.
func (m *FileManager) own(c *fileCtx, f *os.File, dir bool) error {
	if err := m.Chown(f, c.uid, c.gid); err != nil {
		return err
	}
	if c.acl {
		return nil
	}
	mode := fs.FileMode(0o660)
	if dir {
		mode = 0o770
	}
	return f.Chmod(mode)
}

// openDir opens a directory of the share as a descriptor for the *at calls.
func openDir(c *fileCtx, p string) (*os.File, error) {
	name := p
	if name == "" {
		name = "."
	}
	d, err := c.root.Open(name)
	if err != nil {
		return nil, notFound("Složka neexistuje.")
	}
	if st, err := d.Stat(); err != nil || !st.IsDir() {
		d.Close()
		return nil, invalid("path", "Cesta není složka.")
	}
	return d, nil
}

// Mkdir creates a folder (FS-02).
func (m *FileManager) Mkdir(ctx context.Context, user, share, dir, name string) (struct{}, error) {
	if err := checkPath(dir); err != nil {
		return struct{}{}, err
	}
	if err := checkName(name); err != nil {
		return struct{}{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return struct{}{}, err
	}
	defer c.close()
	p := join(dir, name)
	if err := c.root.Mkdir(p, 0o770); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return struct{}{}, existsErr("Položka s tímto názvem už existuje.")
		}
		if errors.Is(err, fs.ErrNotExist) {
			return struct{}{}, notFound("Složka neexistuje.")
		}
		return struct{}{}, internalErr("Složku nelze vytvořit.")
	}
	d, err := c.root.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err == nil {
		err = m.own(c, d, true)
		d.Close()
	}
	if err != nil {
		c.root.Remove(p)
		return struct{}{}, internalErr("Složce nelze nastavit vlastníka.")
	}
	slog.Info("helper: fs.mkdir", "share", share, "user", user)
	return struct{}{}, nil
}

// renameNoReplace moves from/<a> to to/<b> without overwriting.
func renameNoReplace(from *os.File, a string, to *os.File, b string) error {
	return unix.Renameat2(int(from.Fd()), a, int(to.Fd()), b, unix.RENAME_NOREPLACE)
}

func renameErr(err error) error {
	switch {
	case errors.Is(err, fs.ErrExist):
		return existsErr("Položka s tímto názvem už existuje.")
	case errors.Is(err, fs.ErrNotExist):
		return notFound("Položka neexistuje.")
	case errors.Is(err, syscall.EINVAL):
		return invalid("dest", "Složku nelze přesunout do ní samotné.")
	case errors.Is(err, syscall.EXDEV):
		return invalid("dest", "Přesun mezi svazky zatím nejde.")
	}
	return internalErr("Položku nelze přejmenovat ani přesunout.")
}

// Rename renames an entry in its folder (FS-02).
func (m *FileManager) Rename(ctx context.Context, user, share, p, name string) (struct{}, error) {
	if p == "" {
		return struct{}{}, invalid("path", "Kořen sdílené složky nelze přejmenovat.")
	}
	if err := checkPath(p); err != nil {
		return struct{}{}, err
	}
	if err := checkName(name); err != nil {
		return struct{}{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return struct{}{}, err
	}
	defer c.close()
	d, err := openDir(c, parent(p))
	if err != nil {
		return struct{}{}, err
	}
	defer d.Close()
	if path.Base(p) == name {
		return struct{}{}, nil
	}
	if err := renameNoReplace(d, path.Base(p), d, name); err != nil {
		return struct{}{}, renameErr(err)
	}
	slog.Info("helper: fs.rename", "share", share, "user", user)
	return struct{}{}, nil
}

func checkItems(paths []string) error {
	if len(paths) == 0 || len(paths) > MaxFileItems {
		return invalid("paths", "Vyberte 1 až "+strconv.Itoa(MaxFileItems)+" položek.")
	}
	for _, p := range paths {
		if p == "" || !validFilePath(p) {
			return invalid("paths", "Neplatná cesta.")
		}
	}
	return nil
}

func itemErr(p string, err error) FileItemError {
	var he *Error
	if errors.As(err, &he) {
		return FileItemError{Path: p, Message: he.Message}
	}
	return FileItemError{Path: p, Message: "Interní chyba."}
}

// Move moves entries into dest within the share (FS-02). Each entry is
// renamed on its own; existing names are never overwritten.
func (m *FileManager) Move(ctx context.Context, user, share string, paths []string, dest string) (FileBatch, error) {
	if err := checkItems(paths); err != nil {
		return FileBatch{}, err
	}
	if err := checkPath(dest); err != nil {
		return FileBatch{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return FileBatch{}, err
	}
	defer c.close()
	to, err := openDir(c, dest)
	if err != nil {
		return FileBatch{}, err
	}
	defer to.Close()
	res := FileBatch{Failed: []FileItemError{}}
	for _, p := range paths {
		if ctx.Err() != nil {
			res.Failed = append(res.Failed, FileItemError{Path: p, Message: "Přerušeno."})
			continue
		}
		if parent(p) == dest {
			res.Done++
			continue
		}
		from, err := openDir(c, parent(p))
		if err == nil {
			if rerr := renameNoReplace(from, path.Base(p), to, path.Base(p)); rerr != nil {
				err = renameErr(rerr)
			}
			from.Close()
		}
		if err != nil {
			res.Failed = append(res.Failed, itemErr(p, err))
			continue
		}
		res.Done++
	}
	slog.Info("helper: fs.move", "share", share, "done", res.Done, "failed", len(res.Failed), "user", user)
	return res, nil
}

// Delete removes entries (FS-02): into .recycle/<user>/<path> when the
// share has a recycle bin (the Samba layout, emptied by the recycle task),
// otherwise for good.
func (m *FileManager) Delete(ctx context.Context, user, share string, paths []string) (FileBatch, error) {
	if err := checkItems(paths); err != nil {
		return FileBatch{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return FileBatch{}, err
	}
	defer c.close()
	res := FileBatch{Failed: []FileItemError{}, Recycled: c.share.Recycle.Enabled}
	for _, p := range paths {
		if ctx.Err() != nil {
			res.Failed = append(res.Failed, FileItemError{Path: p, Message: "Přerušeno."})
			continue
		}
		var err error
		if c.share.Recycle.Enabled {
			err = m.recycle(ctx, c, p)
		} else {
			err = m.remove(c, p)
		}
		if err != nil {
			res.Failed = append(res.Failed, itemErr(p, err))
			continue
		}
		res.Done++
	}
	slog.Info("helper: fs.delete", "share", share, "done", res.Done, "failed", len(res.Failed), "recycled", res.Recycled, "user", user)
	return res, nil
}

func (m *FileManager) remove(c *fileCtx, p string) error {
	if _, err := c.root.Lstat(p); err != nil {
		return notFound("Položka neexistuje.")
	}
	if err := c.root.RemoveAll(p); err != nil {
		return internalErr("Položku nelze smazat.")
	}
	return nil
}

// mkdirOwned creates the missing folders of p (below the root) for the
// recycle bin, owned by the caller with mode 0700 like Samba's bin.
func (m *FileManager) mkdirOwned(c *fileCtx, p string, mode fs.FileMode, uid int) error {
	cur := ""
	for _, seg := range strings.Split(p, "/") {
		cur = join(cur, seg)
		err := c.root.Mkdir(cur, mode)
		if errors.Is(err, fs.ErrExist) {
			if st, lerr := c.root.Lstat(cur); lerr != nil || !st.IsDir() {
				return internalErr("Koš nelze použít.")
			}
			continue
		}
		if err != nil {
			return internalErr("Koš nelze vytvořit.")
		}
		d, err := c.root.OpenFile(cur, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
		if err == nil {
			if err = m.Chown(d, uid, c.gid); err == nil {
				err = d.Chmod(mode)
			}
			d.Close()
		}
		if err != nil {
			return internalErr("Koš nelze vytvořit.")
		}
	}
	return nil
}

func (m *FileManager) recycle(ctx context.Context, c *fileCtx, p string) error {
	if _, err := c.root.Lstat(p); err != nil {
		return notFound("Položka neexistuje.")
	}
	// .recycle itself is shared by all users (each has a 0700 folder in it).
	if err := m.mkdirOwned(c, recycleDirName, 0o770, 0); err != nil {
		return err
	}
	bin := join(join(recycleDirName, c.user), parent(p))
	if err := m.mkdirOwned(c, bin, 0o700, c.uid); err != nil {
		return err
	}
	from, err := openDir(c, parent(p))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := openDir(c, bin)
	if err != nil {
		return err
	}
	defer to.Close()
	base := path.Base(p)
	target := base
	for n := 1; ; n++ {
		err = renameNoReplace(from, base, to, target)
		if !errors.Is(err, fs.ErrExist) || n > 99 {
			break
		}
		// Samba's recycle:versions naming.
		target = "Copy #" + strconv.Itoa(n) + " of " + base
	}
	if err != nil {
		return renameErr(err)
	}
	m.touch(ctx, c, join(bin, target))
	return nil
}

// touch sets the mtime of the binned files to now (recycle:touch_mtime), so
// the cleanup counts the days from the deletion.
func (m *FileManager) touch(ctx context.Context, c *fileCtx, p string) {
	now := m.Now()
	budget := maxRecycleTouch
	var walk func(p string)
	walk = func(p string) {
		if budget--; budget < 0 || ctx.Err() != nil {
			return
		}
		st, err := c.root.Lstat(p)
		if err != nil {
			return
		}
		switch {
		case st.Mode().IsRegular():
			c.root.Chtimes(p, now, now)
		case st.IsDir():
			d, err := c.root.Open(p)
			if err != nil {
				return
			}
			names, _ := d.Readdirnames(-1)
			d.Close()
			for _, n := range names {
				walk(p + "/" + n)
			}
		}
	}
	walk(p)
}

// UploadOpen creates a hidden temporary file in dir for an upload of size
// bytes and passes its descriptor; UploadCommit gives it its name (FS-03).
func (m *FileManager) UploadOpen(ctx context.Context, user, share, dir, name string, size int64, overwrite bool) (*fileResult, error) {
	if err := checkPath(dir); err != nil {
		return nil, err
	}
	if err := checkName(name); err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, invalid("size", "Neplatná velikost.")
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return nil, err
	}
	defer c.close()
	d, err := openDir(c, dir)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	if st, err := c.root.Lstat(join(dir, name)); err == nil {
		if !overwrite {
			return nil, existsErr("Soubor s tímto názvem už existuje.")
		}
		if !st.Mode().IsRegular() {
			return nil, existsErr("Pod tímto názvem je složka nebo odkaz; přepsat lze jen soubor.")
		}
	}
	if free, err := m.Statfs(d); err == nil && uint64(size)+uploadReserve > free {
		return nil, invalid("size", "Na svazku není dost místa.")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, internalErr("Interní chyba.")
	}
	token := hex.EncodeToString(b)
	tmp := join(dir, uploadPrefix+token)
	f, err := c.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, internalErr("Soubor nelze vytvořit.")
	}
	if err := m.own(c, f, false); err != nil {
		f.Close()
		c.root.Remove(tmp)
		return nil, internalErr("Souboru nelze nastavit vlastníka.")
	}
	return &fileResult{meta: UploadTicket{Token: token}, f: f}, nil
}

// uploadTemp finds the temporary file of token. The token is 128 random bits
// that stay inside the web process (one HTTP request is one upload), and the
// caller needs write access to the share anyway.
func uploadTemp(c *fileCtx, dir, token string) (string, error) {
	if !uploadTokenRe.MatchString(token) {
		return "", invalid("token", "Neplatné nahrávání.")
	}
	tmp := join(dir, uploadPrefix+token)
	st, err := c.root.Lstat(tmp)
	if err != nil || !st.Mode().IsRegular() {
		return "", notFound("Nahrávání neexistuje.")
	}
	return tmp, nil
}

// UploadCommit renames the finished temporary file to name. Access is
// checked again: it may have been revoked during a long upload.
func (m *FileManager) UploadCommit(ctx context.Context, user, share, dir, token, name string, overwrite bool) (struct{}, error) {
	if err := checkPath(dir); err != nil {
		return struct{}{}, err
	}
	if err := checkName(name); err != nil {
		return struct{}{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return struct{}{}, err
	}
	defer c.close()
	tmp, err := uploadTemp(c, dir, token)
	if err != nil {
		return struct{}{}, err
	}
	d, err := openDir(c, dir)
	if err != nil {
		return struct{}{}, err
	}
	defer d.Close()
	if overwrite {
		if st, err := c.root.Lstat(join(dir, name)); err == nil && !st.Mode().IsRegular() {
			c.root.Remove(tmp)
			return struct{}{}, existsErr("Pod tímto názvem je složka nebo odkaz; přepsat lze jen soubor.")
		}
		err = unix.Renameat(int(d.Fd()), path.Base(tmp), int(d.Fd()), name)
	} else {
		err = renameNoReplace(d, path.Base(tmp), d, name)
	}
	if err != nil {
		c.root.Remove(tmp)
		return struct{}{}, renameErr(err)
	}
	slog.Info("helper: fs.upload", "share", share, "overwrite", overwrite, "user", user)
	return struct{}{}, nil
}

// UploadAbort removes an unfinished upload.
func (m *FileManager) UploadAbort(ctx context.Context, user, share, dir, token string) (struct{}, error) {
	if err := checkPath(dir); err != nil {
		return struct{}{}, err
	}
	c, err := m.open(user, share, true)
	if err != nil {
		return struct{}{}, err
	}
	defer c.close()
	tmp, err := uploadTemp(c, dir, token)
	if err != nil {
		return struct{}{}, err
	}
	c.root.Remove(tmp)
	return struct{}{}, nil
}

// SysFileChown is the production Chown.
func SysFileChown(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }

// SysFstatfs reports the bytes available to unprivileged users.
func SysFstatfs(f *os.File) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Fstatfs(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// handleFiles runs the File Station operations.
func (d *Dispatcher) handleFiles(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Share     string   `json:"share"`
		Path      string   `json:"path"`
		Name      string   `json:"name"`
		Paths     []string `json:"paths"`
		Dest      string   `json:"dest"`
		Offset    int      `json:"offset"`
		Size      int64    `json:"size"`
		Overwrite bool     `json:"overwrite"`
		Token     string   `json:"token"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	m := d.Files
	switch req.Op {
	case "fs.shares":
		return m.Shares(ctx, user)
	case "fs.list":
		return m.List(ctx, user, a.Share, a.Path, a.Offset)
	case "fs.open":
		return m.Open(ctx, user, a.Share, a.Path)
	case "fs.mkdir":
		return m.Mkdir(ctx, user, a.Share, a.Path, a.Name)
	case "fs.rename":
		return m.Rename(ctx, user, a.Share, a.Path, a.Name)
	case "fs.move":
		return m.Move(ctx, user, a.Share, a.Paths, a.Dest)
	case "fs.delete":
		return m.Delete(ctx, user, a.Share, a.Paths)
	case "fs.uploadOpen":
		return m.UploadOpen(ctx, user, a.Share, a.Path, a.Name, a.Size, a.Overwrite)
	case "fs.uploadCommit":
		return m.UploadCommit(ctx, user, a.Share, a.Path, a.Token, a.Name, a.Overwrite)
	case "fs.uploadAbort":
		return m.UploadAbort(ctx, user, a.Share, a.Path, a.Token)
	}
	return nil, invalid("", "Neznámá operace.")
}
