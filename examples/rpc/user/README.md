# User Endpoints

[![Back to Main README](https://img.shields.io/badge/←-Back%20to%20Main%20README-blue?style=flat-square)](/examples/rpc/README.md)

Examples for interacting with user-related endpoints.

## 📑 Table of Contents

- [GetUser](#getuser)
  - [Usage](#usage)
  - [Response Fields](#response-fields)
  - [Error Handling](#error-handling)

## GetUser

Retrieves detailed information about a user by their ID.

### Usage

Using Go client:

```bash
go run examples/rpc/user/get_user.go <user_id>
```

Using Bruno:

> See [GetUser.bru](../../../tests/api/user/GetUser.bru) for testing via HTTP/JSON

### Response Fields

Example response:

```json
{
  "status": "USER_STATUS_FLAGGED",
  "user": {
    "id": "123456789",
    "name": "username123",
    "display_name": "Display Name",
    "description": "User's profile description",
    "created_at": "2024-01-01T00:00:00Z",
    "reason": "AI Analysis: Suggestive phrasing and potentially manipulative content warranting further review.",
    "groups": [
      {
        "id": "12345",
        "name": "Group Name",
        "role": "Member"
      }
    ],
    "friends": [
      {
        "id": "67890",
        "name": "Friend Name",
        "display_name": "Friend Display",
        "has_verified_badge": false
      }
    ],
    "games": [
      {
        "id": "55555",
        "name": "User's Place"
      }
    ],
    "flagged_content": ["im 18", "having fun"],
    "follower_count": "10",
    "following_count": "5",
    "confidence": 0.75,
    "last_scanned": "2024-01-01T00:00:00Z",
    "last_updated": "2024-01-01T00:00:00Z",
    "last_viewed": "2024-01-01T00:00:00Z",
    "thumbnail_url": "https://example.com/avatar.jpg",
    "upvotes": 0,
    "downvotes": 0,
    "reputation": 0
  }
}
```

### Error Handling

1. Success response with `status: USER_STATUS_UNFLAGGED` if user doesn't exist:

```json
{
  "status": "USER_STATUS_UNFLAGGED",
  "user": null
}
```

---
[![Back to Main README](https://img.shields.io/badge/←-Back%20to%20Main%20README-blue?style=flat-square)](/examples/rpc/README.md)
