# 🚀 RPC Server Examples

> [!WARNING] > **Rotector is currently in alpha, and this API isn't ready for production use just yet.** The API will be available for testing during the beta phase, but until then, the server and its endpoints may change without notice. We suggest waiting for the beta release before developing any integrations or using the server in a production environment.

This directory contains examples demonstrating how to use the RPC server API for Rotector.

## 📑 Table of Contents

- [🤔 Why RPC over REST?](#-why-rpc-over-rest)
- [🛠️ Language Support](#️-language-support)
- [🔌 API Endpoints](#-api-endpoints)
- [🧪 API Testing](#-api-testing)
- [⚠️ Common Errors](#️-common-errors)
- [❓ FAQ](#-faq)
- [🔗 Links](#-links)

## 🤔 Why RPC over REST?

We opted for RPC instead of a traditional REST API for several reasons. Although REST APIs are commonly utilized in the industry, they often demand considerable effort to keep request and response structures consistent, as well as to maintain documentation across various endpoints. Additionally, they do not offer inherent type safety or code generation features.

We developed our server using [Twirp](https://twitchtv.github.io/twirp/docs/intro.html) v7, an RPC framework created by Twitch that operates over HTTP. Like gRPC, Twirp utilizes Protocol Buffers (protobuf) for defining services and generating code, which ensures type-safe APIs across various programming languages. With a schema-first approach, Twirp greatly minimizes the chances of runtime errors, making both the API contract and documentation originate from the same source, reducing the likelihood of them getting out of sync.

Unlike gRPC, which relies on HTTP/2 and specific tools, Twirp operates on both HTTP/1.1 and HTTP/2, and can be set up without any special configuration. Its support for JSON serialization also simplifies the process of debugging and testing services with regular HTTP tools.

The binary protocol utilized by RPC/Protobuf is also more efficient than HTTP/JSON, resulting in improved performance and reduced bandwidth consumption. Although we offer a HTTP/JSON interface, the RPC interface is better suited for production environments.

## 🛠️ Language Support

Twirp utilizes Protocol Buffers (protobuf) to define services and automatically create client/server code. This allows you to use our [protobuf definitions](https://github.com/rotector/rotector/tree/main/rpc) to generate code in various languages, thanks to Twirp's support for multiple languages.

> [!IMPORTANT]
> We do not offer assistance for setting up client libraries in languages other than Go. Please consult the documentation of the relevant repository for instructions on how to generate and use client code in your language.

Below are the available third-party implementations:

| Language       | Clients | Servers | Repository                                                                                                   |
| -------------- | ------- | ------- | ------------------------------------------------------------------------------------------------------------ |
| **Crystal**    | ✓       | ✓       | [github.com/mloughran/twirp.cr](https://github.com/mloughran/twirp.cr)                                       |
| **Dart**       | ✓       |         | [github.com/apptreesoftware/protoc-gen-twirp_dart](https://github.com/apptreesoftware/protoc-gen-twirp_dart) |
| **Elixir**     | ✓       | ✓       | [github.com/keathley/twirp-elixir](https://github.com/keathley/twirp-elixir)                                 |
| **Java**       | ✓       | ✓       | [github.com/fajran/protoc-gen-twirp_java_jaxrs](https://github.com/fajran/protoc-gen-twirp_java_jaxrs)       |
| **Java**       |         | ✓       | [github.com/devork/flit](https://github.com/devork/flit)                                                     |
| **Java**       |         | ✓       | [github.com/github/flit](https://github.com/github/flit)                                                     |
| **JavaScript** | ✓       |         | [github.com/thechriswalker/protoc-gen-twirp_js](https://github.com/thechriswalker/protoc-gen-twirp_js)       |
| **JavaScript** | ✓       |         | [github.com/Xe/twirp-codegens/cmd/protoc-gen-twirp_jsbrowser](https://github.com/Xe/twirp-codegens)          |
| **JavaScript** | ✓       | ✓       | [github.com/tatethurston/TwirpScript](https://github.com/tatethurston/TwirpScript)                           |
| **Kotlin**     | ✓       |         | [github.com/collectiveidea/twirp-kmm](https://github.com/collectiveidea/twirp-kmm)                           |
| **PHP**        | ✓       | ✓       | [github.com/twirphp/twirp](https://github.com/twirphp/twirp)                                                 |
| **Python3**    | ✓       | ✓       | [github.com/verloop/twirpy](https://github.com/verloop/twirpy)                                               |
| **Ruby**       | ✓       | ✓       | [github.com/twitchtv/twirp-ruby](https://github.com/twitchtv/twirp-ruby)                                     |
| **Rust**       | ✓       | ✓       | [github.com/sourcefrog/prost-twirp](https://github.com/sourcefrog/prost-twirp)                               |
| **Scala**      | ✓       | ✓       | [github.com/soundcloud/twinagle](https://github.com/soundcloud/twinagle)                                     |
| **Swagger**    | ✓       | ✓       | [github.com/go-bridget/twirp-swagger-gen](https://github.com/go-bridget/twirp-swagger-gen)                   |
| **Swift**      | ✓       |         | [github.com/CrazyHulk/protoc-gen-swiftwirp](https://github.com/CrazyHulk/protoc-gen-swiftwirp)               |
| **Typescript** | ✓       | ✓       | [github.com/hopin-team/twirp-ts](https://github.com/hopin-team/twirp-ts)                                     |
| **Typescript** | ✓       | ✓       | [github.com/tatethurston/TwirpScript](https://github.com/tatethurston/TwirpScript)                           |
| **Typescript** | ✓       | ✓       | [github.com/timostamm/protobuf-ts](https://github.com/timostamm/protobuf-ts)                                 |

## 🔌 API Endpoints

### [User Endpoints](user/README.md)

- GetUser: Retrieves user information by ID

## 🧪 API Testing

We use [Bruno](https://usebruno.com/) for testing HTTP/JSON requests, which is an open source API client that's an alternative to Postman. It's fast, lightweight, and stores API collections as files in your project.

### Installation

1. Download from: [https://www.usebruno.com/downloads](https://www.usebruno.com/downloads)
2. Install and open Bruno
3. Open a collection like `tests/api/user`

## ⚠️ Common Errors

### 1. IP Validation Error

```json
{
  "code": "permission_denied",
  "msg": "request must include a valid public IP address"
}
```

This error occurs when the server cannot determine a valid public IP address for the request. Here's how to resolve it:

#### Local Development

- Set `allow_local_ips = true` in your `config/rpc.toml` to allow local IPs (127.0.0.1)
- If using a proxy (like nginx), add your proxy's IP to `trusted_proxies`:
  ```toml
  trusted_proxies = [
    "127.0.0.0/8"  # Local proxy
  ]
  ```
- Ensure your proxy is setting the correct forwarding headers (X-Forwarded-For, etc.)

#### Production Environment

- Configure your reverse proxy/load balancer to set proper forwarding headers
- Add your proxy IPs to the trusted list:
  ```toml
  trusted_proxies = [
    "10.0.0.0/8",      # Internal network
    "172.16.0.0/12",   # Docker network
    "192.168.0.0/16"   # VPC network
  ]
  ```
- If using a CDN, ensure it's setting the appropriate headers and add its IPs to `trusted_proxies`
- Verify that `allow_local_ips = false` for security

### 2. Rate Limit Error

```json
{
  "code": "resource_exhausted",
  "msg": "rate limit exceeded"
}
```

This error can occur in two scenarios:

1. Normal Rate Limiting

   - You've exceeded the default limit of 5 requests per second
   - Solutions:
     - Implement request batching in your client
     - Add delays between requests

2. IP Detection Issues
   - Rate limiting is per-IP, so if IP detection fails, all requests might share the same limit
   - Check if you're getting the IP validation error above
   - Solutions:
     - Fix IP detection issues first
     - Ensure your proxy is properly configured
     - Verify the client IP is being correctly propagated

## ❓ FAQ

### Why are some integer fields returned as strings in JSON?

When using the HTTP/JSON interface, you may notice that some integer fields, such as `follower_count` and `following_count`, are returned as strings, even though they are defined as integers in the protobuf specification:

```json
{
  "follower_count": "10", // string in JSON
  "following_count": "5" // string in JSON
}
```

This behavior is due to Twirp's adherence to the Protocol Buffer specification, which mandates that 64-bit integers (uint64, int64) be encoded as strings in JSON. This ensures compatibility with languages like JavaScript, which cannot safely manage 64-bit integers.

In contrast, when using the recommended RPC/Protobuf method, these values are correctly processed as integers.

## 🔗 Links

### Twirp

- [GitHub Repository](https://github.com/twitchtv/twirp)
- [Official Documentation](https://twitchtv.github.io/twirp/docs/intro.html)
- [Best Practices](https://twitchtv.github.io/twirp/docs/best_practices.html)
- [Protobuf Specification](https://twitchtv.github.io/twirp/docs/spec_v7.html)

### Protocol Buffers

- [Protocol Buffers Documentation](https://protobuf.dev/)
- [Language Guide](https://protobuf.dev/programming-guides/proto3/)
- [JSON Mapping](https://protobuf.dev/programming-guides/proto3/#json)

### Tools

- [Bruno API Client](https://www.usebruno.com/)
- [Protocol Buffer Compiler](https://github.com/protocolbuffers/protobuf?tab=readme-ov-file#protobuf-compiler-installationn)
