package daemon

import "testing"

func TestParseWorkspaceInfoFromSpacesJSON(t *testing.T) {
	data := []byte(`{
	  "SpacesDisplayConfiguration": {
	    "Management Data": {
	      "Monitors": [
	        {
	          "Current Space": {
	            "ManagedSpaceID": 5
	          },
	          "Spaces": [
	            {"ManagedSpaceID": 3},
	            {"ManagedSpaceID": 5},
	            {"ManagedSpaceID": 8}
	          ]
	        }
	      ]
	    }
	  }
	}`)

	current, total, err := parseWorkspaceInfoFromSpacesJSON(data)
	if err != nil {
		t.Fatalf("parse workspace info: %v", err)
	}
	if current != 2 || total != 3 {
		t.Fatalf("got current=%d total=%d, want current=2 total=3", current, total)
	}
}

func TestParseWorkspaceInfoFromSpacesJSONErrorsWhenCurrentMissing(t *testing.T) {
	data := []byte(`{
	  "SpacesDisplayConfiguration": {
	    "Management Data": {
	      "Monitors": [
	        {
	          "Current Space": {
	            "ManagedSpaceID": 9
	          },
	          "Spaces": [
	            {"ManagedSpaceID": 3},
	            {"ManagedSpaceID": 5},
	            {"ManagedSpaceID": 8}
	          ]
	        }
	      ]
	    }
	  }
	}`)

	_, _, err := parseWorkspaceInfoFromSpacesJSON(data)
	if err == nil {
		t.Fatal("expected missing current space error")
	}
}
