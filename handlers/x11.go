//handlers/x11.go
//go:build (linux && !wayland) || !cgo

package handlers

/*
#cgo pkg-config: x11 xfixes
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <sys/types.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/extensions/Xfixes.h>

static Display *dpy = NULL;
static Window win;
static Atom XA_CLIPBOARD;
static Atom XA_UTF8_STRING;
static long last_serial = -1;
static int xfixes_event_base = 0;
static const char *init_error = NULL;

// Initialize X11 resources once. On failure, stores a reason in init_error
// and does not retry — X11 unavailability is treated as permanent.
static void init_x11() {
    if (dpy != NULL) return;
    if (init_error != NULL) return; // already failed, don't retry

    dpy = XOpenDisplay(NULL);
    if (!dpy) {
        init_error = "XOpenDisplay failed: DISPLAY not set or X server not running";
        return;
    }

    int xfixes_error_base;
    if (!XFixesQueryExtension(dpy, &xfixes_event_base, &xfixes_error_base)) {
        XCloseDisplay(dpy);
        dpy = NULL;
        init_error = "XFixesQueryExtension failed: XFixes extension not available on this X server";
        return;
    }

    win = XCreateSimpleWindow(dpy, DefaultRootWindow(dpy), 0, 0, 1, 1, 0, 0, 0);

    XA_CLIPBOARD = XInternAtom(dpy, "CLIPBOARD", False);
    XA_UTF8_STRING = XInternAtom(dpy, "UTF8_STRING", False);

    XFixesSelectSelectionInput(dpy, win, XA_CLIPBOARD, XFixesSetSelectionOwnerNotifyMask);

    // Flush to ensure the request is sent
    XFlush(dpy);
}

const char* getX11InitError() {
    return init_error;
}

// Returns the X11 connection file descriptor for select/poll
int getX11ConnectionFd() {
    init_x11();
    if (!dpy) return -1;
    return ConnectionNumber(dpy);
}

// Returns 1 if clipboard has changed since last check, 0 otherwise
int hasClipboardChangedX11() {
    init_x11();
    if (!dpy) return 0;

    XEvent ev;
    int changed = 0;

    // Process all pending events
    while (XPending(dpy)) {
        XNextEvent(dpy, &ev);

        // XFixes events start at xfixes_event_base
        if (ev.type == xfixes_event_base + XFixesSelectionNotify) {
            XFixesSelectionNotifyEvent *xfe = (XFixesSelectionNotifyEvent *)&ev;
            if (xfe->selection == XA_CLIPBOARD) {
                long serial = xfe->selection_timestamp;
                if (serial != last_serial) {
                    last_serial = serial;
                    changed = 1;
                }
            }
        }
    }

    return changed;
}

// Blocking wait for clipboard change (with timeout in milliseconds).
// Returns: 1 if changed, 0 on timeout, -1 if select() failed, -2 if X11 init failed.
int waitForClipboardChange(int timeout_ms) {
    init_x11();
    if (!dpy) return -2;

    int fd = ConnectionNumber(dpy);
    fd_set fds;
    struct timeval tv;

    tv.tv_sec = timeout_ms / 1000;
    tv.tv_usec = (timeout_ms % 1000) * 1000;

    int ret;
    do {
        FD_ZERO(&fds);
        FD_SET(fd, &fds);
        ret = select(fd + 1, &fds, NULL, NULL, &tv);
    } while (ret == -1 && errno == EINTR); // restart on signal interruption

    if (ret > 0) {
        return hasClipboardChangedX11();
    }

    return ret; // 0 = timeout, -1 = select() error
}

// Predicate for XIfEvent: only match true X11 SelectionNotify (type 31),
// leaving XFixes and other events in the queue for the listener loop.
static Bool isSelectionNotify(Display *d, XEvent *ev, XPointer arg) {
    (void)d; (void)arg;
    return ev->type == SelectionNotify;
}

// Returns clipboard text (UTF-8) or NULL
char* getClipboardTextX11() {
    init_x11();
    if (!dpy) return NULL;

    Atom sel = XA_CLIPBOARD;
    Atom target = XA_UTF8_STRING;

    XConvertSelection(dpy, sel, target, target, win, CurrentTime);
    XFlush(dpy);

    XEvent ev;
    XIfEvent(dpy, &ev, isSelectionNotify, NULL);

    if (ev.type != SelectionNotify) return NULL;
    if (ev.xselection.property == None) return NULL;

    Atom type;
    int format;
    unsigned long len, bytes_left;
    unsigned char *data = NULL;

    XGetWindowProperty(dpy, win, target, 0, ~0, False,
                       AnyPropertyType, &type, &format,
                       &len, &bytes_left, &data);

    if (!data) return NULL;

    char *out = strdup((char*)data);
    XFree(data);
    return out;
}

// Returns true if the atom 'needle' is present in an array of atoms 'haystack'.
static Bool atomInList(Atom needle, Atom *haystack, int n) {
    for (int i = 0; i < n; i++) {
        if (haystack[i] == needle) return True;
    }
    return False;
}

unsigned char* getClipboardImageX11(int *out_len) {
    init_x11();
    if (!dpy) return NULL;

    *out_len = 0;

    Atom sel     = XA_CLIPBOARD;
    Atom TARGETS = XInternAtom(dpy, "TARGETS", False);
    Atom PNG     = XInternAtom(dpy, "image/png",  False);
    Atom JPEG    = XInternAtom(dpy, "image/jpeg", False);

    // Query TARGETS first so we only request formats the owner actually supports.
    // This prevents terminals (e.g. Alacritty) that advertise image/png for their
    // selection screenshots from being mistakenly treated as image copies.
    XConvertSelection(dpy, sel, TARGETS, TARGETS, win, CurrentTime);
    XFlush(dpy);

    XEvent ev;
    XIfEvent(dpy, &ev, isSelectionNotify, NULL);

    if (ev.xselection.property == None) return NULL;

    Atom   type;
    int    format;
    unsigned long len, bytes_left;
    unsigned char *tdata = NULL;

    if (XGetWindowProperty(dpy, win, TARGETS, 0, ~0, False,
                           XA_ATOM, &type, &format,
                           &len, &bytes_left, &tdata) != Success || !tdata) {
        if (tdata) XFree(tdata);
        return NULL;
    }

    Atom *supported = (Atom *)tdata;
    int nsupported  = (int)len;

    Bool hasPNG  = atomInList(PNG,  supported, nsupported);
    Bool hasJPEG = atomInList(JPEG, supported, nsupported);
    XFree(tdata);

    if (!hasPNG && !hasJPEG) return NULL;

    Atom image_targets[] = { PNG, JPEG };
    const int ntargets = 2;

    for (int i = 0; i < ntargets; i++) {
        Atom target = image_targets[i];
        if (target == PNG  && !hasPNG)  continue;
        if (target == JPEG && !hasJPEG) continue;

        XConvertSelection(dpy, sel, target, target, win, CurrentTime);
        XFlush(dpy);

        XIfEvent(dpy, &ev, isSelectionNotify, NULL);

        if (ev.xselection.property == None) continue;

        unsigned char *data = NULL;
        if (XGetWindowProperty(dpy, win, target, 0, ~0, False,
                               AnyPropertyType, &type, &format,
                               &len, &bytes_left, &data) != Success) {
            continue;
        }

        if (!data || len == 0) {
            if (data) XFree(data);
            continue;
        }

        int actual_len = len * (format / 8);
        unsigned char *copy = malloc(actual_len);
        memcpy(copy, data, actual_len);
        XFree(data);

        *out_len = actual_len;
        return copy;
    }

    return NULL;
}

// Clipboard data holder
static unsigned char *clipboard_data = NULL;
static int clipboard_data_len = 0;
static Atom clipboard_data_type;

// Selection handler - responds to requests for our clipboard data
static Bool handleSelectionRequest(XEvent *ev) {
    XSelectionRequestEvent *req = &ev->xselectionrequest;
    XSelectionEvent notify;

    notify.type = SelectionNotify;
    notify.requestor = req->requestor;
    notify.selection = req->selection;
    notify.target = req->target;
    notify.time = req->time;
    notify.property = None;

    Atom TARGETS = XInternAtom(dpy, "TARGETS", False);

    // Handle TARGETS request - tell what formats we support
    if (req->target == TARGETS) {
        Atom supported[] = { clipboard_data_type, TARGETS };
        XChangeProperty(dpy, req->requestor, req->property,
                       XA_ATOM, 32, PropModeReplace,
                       (unsigned char*)supported, 2);
        notify.property = req->property;
    }
    // Handle request for our actual data
    else if (req->target == clipboard_data_type && clipboard_data != NULL) {
        XChangeProperty(dpy, req->requestor, req->property,
                       clipboard_data_type, 8, PropModeReplace,
                       clipboard_data, clipboard_data_len);
        notify.property = req->property;
    }

    XSendEvent(dpy, req->requestor, False, 0, (XEvent*)&notify);
    XFlush(dpy);
    return True;
}

// Set text to clipboard and take ownership. Call serveClipboardUntilLost() afterwards
// to keep serving clipboard requests indefinitely in a dedicated process.
// If sync_primary is non-zero, also take ownership of the PRIMARY selection so
// middle-click paste returns the same content.
int setClipboardTextX11(const char *text, int sync_primary) {
    init_x11();
    if (!dpy || !text) return 0;

    if (clipboard_data) {
        free(clipboard_data);
        clipboard_data = NULL;
    }

    clipboard_data_len = strlen(text);
    clipboard_data = malloc(clipboard_data_len);
    memcpy(clipboard_data, text, clipboard_data_len);
    clipboard_data_type = XA_UTF8_STRING;

    XSetSelectionOwner(dpy, XA_CLIPBOARD, win, CurrentTime);
    XFlush(dpy);

    if (XGetSelectionOwner(dpy, XA_CLIPBOARD) != win) {
        free(clipboard_data);
        clipboard_data = NULL;
        return 0;
    }

    if (sync_primary) {
        XSetSelectionOwner(dpy, XA_PRIMARY, win, CurrentTime);
        XFlush(dpy);
    }

    return 1;
}

// Block serving clipboard SelectionRequests until CLIPBOARD ownership is lost
// (SelectionClear for XA_CLIPBOARD received). SelectionClear for XA_PRIMARY is
// ignored — it just means the user highlighted text somewhere else, which is fine.
// Meant to be called from a dedicated long-lived subprocess immediately after
// setClipboardTextX11.
void serveClipboardUntilLost() {
    if (!dpy) return;
    XEvent ev;
    for (;;) {
        XNextEvent(dpy, &ev);
        if (ev.type == SelectionRequest) {
            handleSelectionRequest(&ev);
        } else if (ev.type == SelectionClear) {
            if (ev.xselectionclear.selection == XA_CLIPBOARD) {
                if (clipboard_data) {
                    free(clipboard_data);
                    clipboard_data = NULL;
                }
                return;
            }
            // SelectionClear for PRIMARY: user selected text elsewhere — keep serving CLIPBOARD.
        }
    }
}

// setClipboardImageX11 takes ownership of the X11 CLIPBOARD selection with the given
// image data. Call serveClipboardUntilLost() afterwards in a dedicated long-lived
// subprocess to keep serving SelectionRequests indefinitely.
int setClipboardImageX11(unsigned char *data, int len, const char *mime_type) {
    init_x11();
    if (!dpy || !data || len <= 0) return 0;

    if (clipboard_data) {
        free(clipboard_data);
        clipboard_data = NULL;
    }

    clipboard_data_len = len;
    clipboard_data = malloc(len);
    memcpy(clipboard_data, data, len);
    clipboard_data_type = XInternAtom(dpy, mime_type, False);

    XSetSelectionOwner(dpy, XA_CLIPBOARD, win, CurrentTime);
    XFlush(dpy);

    if (XGetSelectionOwner(dpy, XA_CLIPBOARD) != win) {
        free(clipboard_data);
        clipboard_data = NULL;
        return 0;
    }

    return 1;
}
*/
import "C"
import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/savedra1/clipse/config"
	"github.com/savedra1/clipse/shell"
	"github.com/savedra1/clipse/utils"
)

func X11GetClipboardText() string {
	cstr := C.getClipboardTextX11()
	if cstr == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr)
}

func X11ClipboardChanged() bool {
	return C.hasClipboardChangedX11() != 0
}

// RunX11Listener blocks, monitoring the X11 clipboard for changes and saving new
// entries to history. It uses select(2) on the X11 connection fd for efficient
// blocking rather than polling.
func RunX11Listener() {
	for {
		result := int(C.waitForClipboardChange(1000))

		switch {
		case result > 0:
			imgContents, err := GetClipboardImage()
			if err != nil {
				utils.LogERROR(fmt.Sprintf("error getting clipboard image: %v", err))
			}
			if imgContents != nil {
				utils.HandleError(SaveImage(imgContents))
			}

			activeWindow := shell.X11ActiveWindowTitle()
			if isAppExcluded(activeWindow, config.ClipseConfig.ExcludedApps) {
				utils.LogINFO(fmt.Sprintf("skipping clipboard content from excluded app: %s", activeWindow))
				continue
			}

			textContents := X11GetClipboardText()
			if textContents != "" {
				utils.HandleError(SaveText(textContents))
			}

		case result == 0:
			// Timeout — no clipboard change, loop normally.

		case result == -2:
			// X11 init failed permanently (e.g. DISPLAY not set). Nothing to retry.
			reason := C.GoString(C.getX11InitError())
			utils.LogERROR(fmt.Sprintf("X11 initialisation failed: %s", reason))
			return

		default:
			// Transient select(2) error — log and retry after a short back-off.
			utils.LogERROR("error waiting for clipboard change: select() failed")
			time.Sleep(time.Second)
		}
	}
}

func GetClipboardImage() ([]byte, error) {
	var outLen C.int

	ptr := C.getClipboardImageX11(&outLen)
	if ptr == nil || outLen == 0 {
		return nil, nil
	}

	buf := C.GoBytes(unsafe.Pointer(ptr), outLen)
	C.free(unsafe.Pointer(ptr))

	return buf, nil
}

func X11SetClipboardText(text string) {
	exe, err := os.Executable()
	if err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard server: failed to find own executable: %v", err))
		return
	}
	cmd := exec.Command(exe, "--serve-x11-clipboard")
	// Use StdinPipe and write synchronously so the data is guaranteed to be in
	// the pipe buffer before this process exits (avoids a race with cmd.Stdin +
	// internal goroutine when the parent is a short-lived process like clipse -c).
	pw, err := cmd.StdinPipe()
	if err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard server: failed to create stdin pipe: %v", err))
		return
	}
	// Detach subprocess from terminal so it survives the parent process exiting.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard server: failed to launch subprocess: %v", err))
		return
	}
	if _, err := io.WriteString(pw, text); err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard server: failed to write text to pipe: %v", err))
	}
	pw.Close()
	_ = cmd.Process.Release()
}

// RunX11ClipboardServer takes X11 clipboard ownership for the given text and blocks
// serving SelectionRequest events until another application takes ownership
// (SelectionClear). It is intended to run in a dedicated subprocess launched by
// X11SetClipboardText.
func RunX11ClipboardServer(text string) {
	cstr := C.CString(text)
	defer C.free(unsafe.Pointer(cstr))

	syncPrimary := C.int(0)
	if config.ClipseConfig.SyncPrimarySelection {
		syncPrimary = C.int(1)
	}

	if C.setClipboardTextX11(cstr, syncPrimary) == 0 {
		utils.LogERROR("clipboard server: failed to take X11 clipboard ownership")
		return
	}
	C.serveClipboardUntilLost()
}

func X11Paste() {
	imgContents, err := GetClipboardImage()
	utils.HandleError(err)

	if imgContents != nil {
		fmt.Println(string(imgContents))
		return
	}

	textContents := X11GetClipboardText()
	fmt.Println(textContents)
	return
}

func X11SetClipboardImage(filePath string) {
	exe, err := os.Executable()
	if err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard image server: failed to find own executable: %v", err))
		return
	}
	cmd := exec.Command(exe, "--serve-x11-clipboard-image", filePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard image server: failed to launch subprocess: %v", err))
		return
	}
	_ = cmd.Process.Release()
}

// RunX11ClipboardImageServer takes X11 clipboard ownership for the given image file
// and blocks serving SelectionRequest events until another application takes ownership
// (SelectionClear). It is intended to run in a dedicated subprocess launched by
// X11SetClipboardImage.
func RunX11ClipboardImageServer(filePath string) {
	imgData, err := os.ReadFile(filePath)
	if err != nil {
		utils.LogERROR(fmt.Sprintf("clipboard image server: failed to read image: %v", err))
		return
	}
	if len(imgData) == 0 {
		utils.LogWARN("clipboard image server: empty image data")
		return
	}

	cmime := C.CString("image/png")
	defer C.free(unsafe.Pointer(cmime))

	cdata := C.CBytes(imgData)
	defer C.free(cdata)

	if C.setClipboardImageX11((*C.uchar)(cdata), C.int(len(imgData)), cmime) == 0 {
		utils.LogERROR("clipboard image server: failed to take X11 clipboard ownership")
		return
	}
	C.serveClipboardUntilLost()
}
