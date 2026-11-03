---
name: api-testing
description: "Use when testing, validating, or debugging a REST API endpoint, when setting up automated API tests, when choosing between curl/httpie/Postman-style tooling, or when writing API test documentation."
version: 1.1.0
author: go-magic
license: MIT
tags: [api, testing, rest, automation]
tools: [execute_command, read_file]
---

# API Testing Guide

Guide for testing RESTful APIs: quick manual checks, automated suites, and the failure cases people forget.

## When to Use

Load this skill when:
- Testing REST API endpoints by hand or in a suite
- Validating responses, status codes, and schemas
- Setting up automated API tests
- Choosing API testing tooling
- Debugging a request/response problem

## Quick Reference

### curl (quick manual checks)
```bash
# GET
curl -X GET https://api.example.com/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json"

# POST
curl -X POST https://api.example.com/users \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name": "test", "email": "test@example.com"}'

# PUT
curl -X PUT https://api.example.com/users/123 \
  -H "Content-Type: application/json" \
  -d '{"name": "updated"}'

# DELETE
curl -X DELETE https://api.example.com/users/123 \
  -H "Authorization: Bearer $TOKEN"

# Query parameters
curl "https://api.example.com/users?page=1&limit=10"

# Show status code and timing without the body noise
curl -s -o /dev/null -w '%{http_code} %{time_total}s\n' https://api.example.com/health
```

Note: if the shell has an HTTP proxy set (common on dev machines), localhost calls may be hijacked by it — add `--noproxy '*'` when testing a local server.

### httpie (friendlier output)
```bash
# pip install httpie
http GET https://api.example.com/users Authorization:"Bearer $TOKEN"
http POST https://api.example.com/users name="test" email="test@example.com"
```

## In This Repo (Go)

API tests here are Go tests, not a separate framework — keep them in the same package as the code (`*_test.go`) and drive `net/http/httptest`:

```go
func TestGetUser(t *testing.T) {
	srv := httptest.NewServer(newTestRouter()) // or call the handler directly
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/users/1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got User
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("id = %d, want 1", got.ID)
	}
}
```

Run with `go test ./...`, and `go test -race ./...` when the handler touches shared state. Table-driven cases are the norm for per-endpoint coverage.

## Testing Tools

### GUI
| Tool | Platform | Best for |
|------|----------|----------|
| Postman | All | Full-featured, collections, team sharing |
| Insomnia | All | Lightweight, GraphQL |
| Bruno | All | Open-source, offline-first, files in git |
| Hoppscotch | Web | Quick online checks |

### CLI
| Tool | Install | Notes |
|------|---------|-------|
| curl | built-in | Universal, scriptable |
| httpie | `pip install httpie` | Human-friendly output |
| xh | `cargo install xh` | Rust httpie |

### Frameworks
| Language | Framework | Install |
|----------|-----------|---------|
| Go | `net/http` + `testing` + `httptest` | built-in |
| Python | pytest + requests | `pip install pytest requests` |
| JavaScript | Jest + supertest | `npm i -D jest supertest` |
| Java | JUnit + RestAssured | Maven/Gradle |

## Best Practices

### Cover every method
- ✅ GET: retrieval, status 200, empty result is also a case
- ✅ POST: creation, status 201, and what happens on a duplicate
- ✅ PUT/PATCH: update semantics, partial update behavior
- ✅ DELETE: 204 or 200, and that a second delete is 404
- ✅ HEAD/OPTIONS: metadata and CORS preflight

### Validate the response
```python
def test_get_user():
    r = requests.get(f"{BASE_URL}/users/1")
    assert r.status_code == 200
    assert "application/json" in r.headers["Content-Type"]
    data = r.json()
    assert isinstance(data["id"], int)
    assert "name" in data

def test_get_user_not_found():
    assert requests.get(f"{BASE_URL}/users/99999").status_code == 404
```

### Test the failure cases
- ❌ Invalid input: wrong types, missing required fields
- ❌ Boundary input: empty strings, max lengths, zero, negative
- ❌ Auth: missing / invalid / expired token
- ❌ Authorization: valid token, insufficient permission (403, not 404-leak)
- ❌ Not found: wrong IDs
- ❌ Conflict: duplicate unique fields
- ❌ Rate limiting: behavior once the limit is hit

### Checklist
- [ ] Every endpoint covered
- [ ] Success responses validated (status + schema, not just 200)
- [ ] Error responses tested
- [ ] Auth / authorization verified
- [ ] Input validation checked
- [ ] Idempotency checked (the suite can run repeatedly)
- [ ] Rate limiting behavior verified

## Automated Testing Example

### Go (table-driven)
```go
func TestUsersAPI(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"list users", http.MethodGet, "/users", "", http.StatusOK},
		{"missing user", http.MethodGet, "/users/99999", "", http.StatusNotFound},
		{"create user", http.MethodPost, "/users", `{"name":"T"}`, http.StatusCreated},
		{"create invalid", http.MethodPost, "/users", `{}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			rec := httptest.NewRecorder()
			newTestRouter().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
```

### Python (pytest + requests)
```python
import pytest, requests

BASE_URL = "https://api.example.com"

@pytest.fixture
def auth_headers():
    return {"Authorization": "Bearer test-token"}

class TestUsersAPI:
    def test_get_users(self, auth_headers):
        r = requests.get(f"{BASE_URL}/users", headers=auth_headers)
        assert r.status_code == 200
        assert isinstance(r.json(), list)

    def test_create_user(self, auth_headers):
        r = requests.post(f"{BASE_URL}/users",
                          json={"name": "Test User", "email": "t@example.com"},
                          headers=auth_headers)
        assert r.status_code == 201
        assert r.json()["name"] == "Test User"
```

## Pitfalls

### Ignoring status codes
**Problem**: asserting on the body while a 500 sails through.
**Solution**: assert the status first, then the body.

### Only testing the happy path with valid auth
**Problem**: the failure branches are exactly where the bugs are.
**Solution**: add missing/invalid/expired token cases, and 403 vs 404.

### Hardcoded test data
**Problem**: tests break when fixtures change.
**Solution**: generate data per test, and clean up what you create.

### Skipping teardown
**Problem**: created resources accumulate and cause conflicts on re-run.
**Solution**: delete what you created, or use a rolled-back transaction.

## Verification

1. Run the suite: `go test ./...` (or `pytest` / `npm test`)
2. Confirm every endpoint has at least one success and one failure case
3. Check coverage of the handler package
4. Re-run the suite twice to confirm idempotency
5. Run with `-race` when handlers share state

## References

- [REST API Tutorial](https://restapitutorial.com/)
- [httpie docs](https://httpie.io/docs)
- [Go httptest](https://pkg.go.dev/net/http/httptest)
- [pytest docs](https://docs.pytest.org/)
- [Supertest](https://github.com/visionmedia/supertest)
