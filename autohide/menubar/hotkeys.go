package menubar

/*
#cgo LDFLAGS: -framework Carbon

#include <Carbon/Carbon.h>

extern void hotkeyPressed(int hotkeyID);

enum {
	hotKeyModifiersCtrlShift = controlKey | shiftKey,
	hotKeyModifiersHyper = cmdKey | controlKey | optionKey | shiftKey,
	hotKeyCodeO = kVK_ANSI_O,
	hotKeyCodeN = kVK_ANSI_N,
};

static OSStatus hotKeyHandler(EventHandlerCallRef nextHandler, EventRef event, void *userData) {
	EventHotKeyID hotKeyID;
	GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(hotKeyID), NULL, &hotKeyID);
	hotkeyPressed((int)hotKeyID.id);
	return noErr;
}

static OSStatus installHotKeyHandler(void) {
	EventTypeSpec eventType = {kEventClassKeyboard, kEventHotKeyPressed};
	return InstallApplicationEventHandler(&hotKeyHandler, 1, &eventType, NULL, NULL);
}

static OSStatus registerWorkspaceHotKey(uint32_t signature, uint32_t hotKeyID, uint32_t keyCode, uint32_t modifiers) {
	EventHotKeyID eventID;
	EventHotKeyRef ref;

	eventID.signature = signature;
	eventID.id = hotKeyID;
	return RegisterEventHotKey(keyCode, modifiers, eventID, GetApplicationEventTarget(), 0, &ref);
}
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"autohide/daemon"
)

const hotKeySignature = 0x4155544F

const (
	hotKeySwitchCtrlShiftO = 1
	hotKeySwitchHyperO     = 2
	hotKeyNameHyperN       = 3
)

type hotKeySpec struct {
	id          int
	description string
	keyCode     uint32
	modifiers   uint32
	handler     func()
}

var (
	hotKeyOnce sync.Once
	hotKeyErr  error
	hotKeyMap  = map[int]func(){}
)

func registerHotkeys() error {
	hotKeyOnce.Do(func() {
		specs := []hotKeySpec{
			{
				id:          hotKeySwitchCtrlShiftO,
				description: "Control+Shift+O",
				keyCode:     uint32(C.hotKeyCodeO),
				modifiers:   uint32(C.hotKeyModifiersCtrlShift),
				handler:     openWorkspaceSwitcher,
			},
			{
				id:          hotKeySwitchHyperO,
				description: "Hyper+O",
				keyCode:     uint32(C.hotKeyCodeO),
				modifiers:   uint32(C.hotKeyModifiersHyper),
				handler:     openWorkspaceSwitcher,
			},
			{
				id:          hotKeyNameHyperN,
				description: "Hyper+N",
				keyCode:     uint32(C.hotKeyCodeN),
				modifiers:   uint32(C.hotKeyModifiersHyper),
				handler:     promptCurrentWorkspaceLabel,
			},
		}

		if status := C.installHotKeyHandler(); status != C.noErr {
			hotKeyErr = fmt.Errorf("install Carbon hotkey handler: %d", int(status))
			return
		}

		var failures []string
		for _, spec := range specs {
			hotKeyMap[spec.id] = spec.handler
			status := C.registerWorkspaceHotKey(
				C.uint32_t(hotKeySignature),
				C.uint32_t(spec.id),
				C.uint32_t(spec.keyCode),
				C.uint32_t(spec.modifiers),
			)
			if status != C.noErr {
				failures = append(failures, fmt.Sprintf("%s (%d)", spec.description, int(status)))
			}
		}

		if len(failures) > 0 {
			hotKeyErr = fmt.Errorf("failed to register: %s", strings.Join(failures, ", "))
		}
	})
	return hotKeyErr
}

func promptCurrentWorkspaceLabel() {
	current, err := daemon.GetCurrentWorkspaceNumber()
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace label hotkey: %v\n", err)
		return
	}
	go promptWorkspaceLabel(current, dm.Config().WorkspaceLabel(current))
}

//export hotkeyPressed
func hotkeyPressed(hotKeyID C.int) {
	handler := hotKeyMap[int(hotKeyID)]
	if handler == nil {
		return
	}
	go handler()
}
