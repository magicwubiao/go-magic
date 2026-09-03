---
name: api-testing
description: "API testing best practices, tools, and workflows for RESTful API testing."
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [api, testing, rest, automation]
    category: software-development
---

# API Testing Guide

Comprehensive guide for testing RESTful APIs using various tools and frameworks.

## When to Use

Load this skill when:
- Testing REST API endpoints
- Validating API responses and status codes
- Setting up automated API tests
- Choosing API testing tools
- Debugging API request/response issues
- Writing API test documentation

## Quick Reference

### curl (Quick Tests)
```bash
# GET request
curl -X GET https://api.example.com/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json"

# POST request
curl -X POST https://api.example.com/users \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name": "test", "email": "test@example.com"}'

# PUT request
curl -X PUT https://api.example.com/users/123 \
  -H "Content-Type: application/json" \
  -d '{"name": "updated"}'

# DELETE request
curl -X DELETE https://api.example.com/users/123 \
  -H "Authorization: Bearer $TOKEN"

# With query parameters
curl "https://api.example.com/users?page=1&limit=10"
```

### httpie (Friendlier curl Alternative)
```bash
# Install: pip install httpie

# GET
http GET https://api.example.com/users Authorization:"Bearer $TOKEN"

# POST with JSON
http POST https://api.example.com/users \
  name="test" email="test@example.com" \
  Authorization:"Bearer $TOKEN"
```

## Testing Tools

### GUI Tools
| Tool | Platform | Best For |
|------|----------|----------|
| Postman | All | Full-featured API testing, collections |
| Insomnia | All | Lightweight, GraphQL support |
| Bruno | All | Open-source, offline-first |
| Hoppscotch | Web | Quick online testing |

### CLI Tools
| Tool | Install | Features |
|------|--------|---------|
| curl | Built-in | Universal, scripting-friendly |
| httpie | `pip install httpie` | Human-friendly output |
| xh | `cargo install xh` | Rust version of httpie |

### Automation Frameworks
| Language | Framework | Install |
|----------|-----------|--------|
| Python | pytest + requests | `pip install pytest requests` |
| JavaScript | Jest + supertest | `npm install --save-dev jest supertest` |
| Go | net/http + testing | Built-in |
| Java | JUnit + RestAssured | Maven/Gradle |

## Best Practices

### Test All HTTP Methods
- ✅ **GET**: Verify data retrieval, status 200
- ✅ **POST**: Test creation, status 201
- ✅ **PUT/PATCH**: Test updates, status 200
- ✅ **DELETE**: Test deletion, status 204 or 200
- ✅ **HEAD/OPTIONS**: Test metadata endpoints

### Validate Response
```python
# Python example
import requests

def test_get_user():
    response = requests.get(f"{BASE_URL}/users/1")
    
    # Status code
    assert response.status_code == 200
    
    # Headers
    assert "application/json" in response.headers["Content-Type"]
    
    # Schema
    data = response.json()
    assert "id" in data
    assert "name" in data
    assert isinstance(data["id"], int)
    
    # Error cases
    response = requests.get(f"{BASE_URL}/users/99999")
    assert response.status_code == 404
```

### Test Error Cases
- ❌ Invalid input (wrong types, missing fields)
- ❌ Edge cases (empty strings, max lengths)
- ❌ Authentication failures (invalid/missing tokens)
- ❌ Authorization failures (insufficient permissions)
- ❌ Resource not found (wrong IDs)
- ❌ Conflict cases (duplicate unique fields)

### Checklist
- [ ] All endpoints covered
- [ ] Success responses validated
- [ ] Error responses tested
- [ ] Authentication/authorization verified
- [ ] Input validation checked
- [ ] Response schema validated
- [ ] Performance benchmarks (if needed)
- [ ] Rate limiting behavior tested

## Automated Testing Example

### Python (pytest + requests)
```python
import pytest
import requests

BASE_URL = "https://api.example.com"

class TestUsersAPI:
    @pytest.fixture
    def auth_headers(self):
        return {"Authorization": "Bearer test-token"}
    
    def test_get_users(self, auth_headers):
        response = requests.get(f"{BASE_URL}/users", headers=auth_headers)
        assert response.status_code == 200
        assert isinstance(response.json(), list)
    
    def test_create_user(self, auth_headers):
        data = {"name": "Test User", "email": "test@example.com"}
        response = requests.post(f"{BASE_URL}/users", 
                                  json=data, headers=auth_headers)
        assert response.status_code == 201
        assert response.json()["name"] == "Test User"
    
    def test_get_user_not_found(self, auth_headers):
        response = requests.get(f"{BASE_URL}/users/99999", headers=auth_headers)
        assert response.status_code == 404
```

### JavaScript (Jest + supertest)
```javascript
const request = require('supertest');
const app = require('../app');

describe('Users API', () => {
  let authToken;
  
  beforeAll(async () => {
    const res = await request(app)
      .post('/auth/login')
      .send({ username: 'test', password: 'test' });
    authToken = res.body.token;
  });
  
  test('GET /users returns list', async () => {
    const res = await request(app)
      .get('/users')
      .set('Authorization', `Bearer ${authToken}`);
    expect(res.statusCode).toBe(200);
    expect(Array.isArray(res.body)).toBe(true);
  });
});
```

## Pitfalls

### Ignoring Status Codes
**Problem**: Only checking response body, not status codes  
**Solution**: Always assert status codes first

### Not Testing Auth
**Problem**: Only testing with valid auth, missing auth failure cases  
**Solution**: Add tests for missing/invalid/expired tokens

### Hardcoding Test Data
**Problem**: Tests fail when data changes  
**Solution**: Use fixtures or factory patterns to generate test data

### Skipping Teardown
**Problem**: Test data accumulates, causing conflicts  
**Solution**: Clean up (delete) created resources in teardown

## Verification

After setting up API tests:
1. Run full test suite: `pytest` or `npm test`
2. Verify all endpoints covered
3. Check code coverage: `pytest --cov`
4. Test error cases manually with curl
5. Verify tests are idempotent (can run multiple times)

## Tools & References

- [Postman Learning Center](https://learning.postman.com/)
- [REST API Tutorial](https://restapitutorial.com/)
- [httpie Documentation](https://httpie.io/docs)
- [pytest Documentation](https://docs.pytest.org/)
- [Supertest GitHub](https://github.com/visionmedia/supertest)
