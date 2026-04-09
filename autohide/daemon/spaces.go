package daemon

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <CoreFoundation/CoreFoundation.h>

typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void);
extern uint64_t CGSGetActiveSpace(CGSConnectionID cid);
extern CFArrayRef CGSCopyManagedDisplaySpaces(CGSConnectionID cid);

// getWorkspaceInfo returns the 1-based workspace index for the active space and
// the total workspace count for the display that owns that active space.
// Returns 0 for both values if the active space is not found.
static void getWorkspaceInfo(int *current, int *count) {
    *current = 0;
    *count = 0;

    CGSConnectionID conn = CGSMainConnectionID();
    if (conn == 0) return;

    uint64_t activeSpace = CGSGetActiveSpace(conn);
    CFArrayRef displaySpaces = CGSCopyManagedDisplaySpaces(conn);
    if (!displaySpaces) return;

    CFIndex displayCount = CFArrayGetCount(displaySpaces);

    for (CFIndex d = 0; d < displayCount; d++) {
        CFDictionaryRef display = CFArrayGetValueAtIndex(displaySpaces, d);

        // Get the "Spaces" array from this display
        CFArrayRef spaces = CFDictionaryGetValue(display, CFSTR("Spaces"));
        if (!spaces) continue;

        CFIndex spaceCount = CFArrayGetCount(spaces);
        for (CFIndex i = 0; i < spaceCount; i++) {
            CFDictionaryRef space = CFArrayGetValueAtIndex(spaces, i);
            CFNumberRef idRef = CFDictionaryGetValue(space, CFSTR("ManagedSpaceID"));
            if (!idRef) continue;

            int64_t spaceId = 0;
            CFNumberGetValue(idRef, kCFNumberSInt64Type, &spaceId);

            if ((uint64_t)spaceId == activeSpace) {
                *current = (int)(i + 1);
                *count = (int)spaceCount;
                break;
            }
        }
        if (*current > 0) break;
    }

    CFRelease(displaySpaces);
}
*/
import "C"

import "fmt"

type Workspace struct {
	Number  int
	Current bool
}

func GetWorkspaceInfo() (int, int, error) {
	var current C.int
	var total C.int
	C.getWorkspaceInfo(&current, &total)
	if current == 0 || total == 0 {
		return 0, 0, fmt.Errorf("could not determine current workspace")
	}
	return int(current), int(total), nil
}

// GetCurrentWorkspaceNumber returns the 1-based macOS workspace (Space) number.
func GetCurrentWorkspaceNumber() (int, error) {
	current, _, err := GetWorkspaceInfo()
	return current, err
}

func ListCurrentDisplayWorkspaces() ([]Workspace, int, error) {
	current, total, err := GetWorkspaceInfo()
	if err != nil {
		return nil, 0, err
	}

	workspaces := make([]Workspace, 0, total)
	for n := 1; n <= total; n++ {
		workspaces = append(workspaces, Workspace{
			Number:  n,
			Current: n == current,
		})
	}
	return workspaces, current, nil
}
