package daemon

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation

#include <CoreGraphics/CoreGraphics.h>
#include <CoreFoundation/CoreFoundation.h>

typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void);
extern uint64_t CGSGetActiveSpace(CGSConnectionID cid);
extern CFArrayRef CGSCopyManagedDisplaySpaces(CGSConnectionID cid);

// getWorkspaceNumber returns the 1-based workspace index for the active space.
// Returns 0 if the active space is not found in any display's space list.
static int getWorkspaceNumber() {
    CGSConnectionID conn = CGSMainConnectionID();
    uint64_t activeSpace = CGSGetActiveSpace(conn);

    CFArrayRef displaySpaces = CGSCopyManagedDisplaySpaces(conn);
    if (!displaySpaces) return 0;

    int result = 0;
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
                result = (int)(i + 1);
                break;
            }
        }
        if (result > 0) break;
    }

    CFRelease(displaySpaces);
    return result;
}
*/
import "C"

import "fmt"

// GetCurrentWorkspaceNumber returns the 1-based macOS workspace (Space) number.
func GetCurrentWorkspaceNumber() (int, error) {
	n := int(C.getWorkspaceNumber())
	if n == 0 {
		return 0, fmt.Errorf("could not determine current workspace")
	}
	return n, nil
}
