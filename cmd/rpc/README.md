# 🚀 RPC Server

> [!WARNING]
> **This API is still in development!** The API will be available for testing during the beta phase, but until then, the server and its endpoints may change without notice. We suggest waiting for the beta release before developing any integrations or using the server in a production environment.

## 📑 Table of Contents

- [🤔 Why RPC over REST?](#-why-rpc-over-rest)
- [🛠️ Language Support](#️-language-support)
- [📚 Examples](#-examples)
- [🔗 Links](#-links)

## 🤔 Why RPC over REST?

Although REST APIs are commonly utilized in the industry, they often demand considerable effort to keep request and response structures consistent, and do not offer inherent type safety or code generation features.

This RPC server uses [Twirp](https://twitchtv.github.io/twirp/docs/intro.html) v7, an RPC framework created by Twitch that operates over HTTP. Like gRPC, Twirp utilizes Protocol Buffers (protobuf) for defining services and generating code, which ensures type-safe APIs across various programming languages. Twirp also operates on both HTTP/1.1 and HTTP/2, and can be set up without any special configuration.

The binary protocol utilized by RPC/Protobuf is more efficient than HTTP/JSON, resulting in improved performance and reduced bandwidth consumption. Although we offer a REST API, the RPC interface is better suited for production environments.

## 🛠️ Language Support

Twirp utilizes Protocol Buffers (protobuf) to define services and automatically create client/server code. This allows you to use our [protobuf definitions](https://github.com/rotector/rotector/tree/main/internal/rpc/proto) to generate code in various languages, thanks to Twirp's support for multiple languages.

> [!IMPORTANT]
> We do not offer assistance for setting up client libraries in languages other than Go. Please consult the documentation of the relevant repository for instructions on how to generate and use client code in your language.

Below are the available third-party implementations:

| Language       | Clients | Servers | Repository                                                                                                   |
|----------------|---------|---------|--------------------------------------------------------------------------------------------------------------|
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

## 📚 Examples

The following examples demonstrate how to use the RPC API.

### User Operations

- [Get User](../../examples/rpc/user/get_user.go) - Retrieve user information by ID

### Group Operations

- [Get Group](../../examples/rpc/group/get_group.go) - Retrieve group information by ID

## 🔗 Links

### Twirp

- [GitHub Repository](https://github.com/twitchtv/twirp)
- [Official Documentation](https://twitchtv.github.io/twirp/docs/intro.html)
- [Best Practices](https://twitchtv.github.io/twirp/docs/best_practices.html)
- [Protobuf Specification](https://twitchtv.github.io/twirp/docs/spec_v7.html)

### Protocol Buffers

- [Protocol Buffers Documentation](https://protobuf.dev/)
- [Protocol Buffer Compiler](https://github.com/protocolbuffers/protobuf?tab=readme-ov-file#protobuf-compiler-installationn)
- [Language Guide](https://protobuf.dev/programming-guides/proto3/)
