<h1 align="center">
  <picture>
    <img width="800" alt="Rotector" src="./assets/images/banner.gif">
  </picture>
  <br>
  <a href="https://github.com/rotector/rotector/blob/main/LICENSE">
    <img src="https://img.shields.io/github/license/rotector/rotector?style=flat-square&color=4a92e1">
  </a>
  <a href="https://github.com/rotector/rotector/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/rotector/rotector/ci.yml?style=flat-square&color=4a92e1">
  </a>
  <a href="https://github.com/rotector/rotector/issues">
    <img src="https://img.shields.io/github/issues/rotector/rotector?style=flat-square&color=4a92e1">
  </a>
  <a href="CODE_OF_CONDUCT.md">
    <img src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4a92e1?style=flat-square">
  </a>
</h1>

<p align="center">
  <em>When Roblox moderators dream of superpowers, they dream of <b>Rotector</b>. A powerful application built with <a href="https://go.dev/">Go</a> that uses AI and smart algorithms to find inappropriate Roblox accounts.</em>
  <br><br>
  🚀 <strong>An experimental project built with modern technologies.</strong>
</p>

---

> [!IMPORTANT]
> This project is currently in an **ALPHA** state with frequent breaking changes expected. Issues are currently disabled as the team focuses on implementing core features. This is a **community-driven initiative** and is not affiliated with, endorsed by, or sponsored by Roblox Corporation. More details in the [Disclaimer](#%EF%B8%8F-disclaimer) section.

---

## 📚 Table of Contents

- [🔍 Overview](#-overview)
- [🚀 Features](#-features)
- [📦 Prerequisites](#-prerequisites)
- [🎯 Accuracy](#-accuracy)
- [⚡ Efficiency](#-efficiency)
- [🔄 Reviewing](#-reviewing)
- [🛣️ Roadmap](#️-roadmap)
- [❓ FAQ](#-faq)
- [👥 Contributing](#-contributing)
- [📄 License](#-license)
- [⚠️ Disclaimer](#%EF%B8%8F-disclaimer)

## 🔍 Overview

|                                                                                                                                                 Swift AI Assisted Moderation                                                                                                                                                  |                                                                                                                                      In-Depth User Investigation                                                                                                                                       |
| :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: | :----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: |
|                       <p align="center"><img src="assets/images/01.gif" width="450"></p><p align="center">Review flagged accounts quickly with AI help. The system shows profile details and AI-detected violations, helping moderators make quick and smart decisions.</p>                       |       <p align="center"><img src="assets/images/02.gif" width="450"></p><p align="center">The review menu also allows in-depth user investigation. Moderators can easily explore a user's outfits, friends, and groups, providing a detailed view for thorough and informed decision-making.</p>       |
|                                                                                                                                                         Streamer Mode                                                                                                                                                         |                                                                                                                                            User Log Browser                                                                                                                                            |
| <p align="center"><img src="assets/images/03.gif" width="450"></p><p align="center">Streamer mode provides additional privacy by censoring sensitive user information in the review menu. This feature is particularly useful for content creators and streamers who want to showcase the tool while maintaining privacy.</p> | <p align="center"><img src="assets/images/04.gif" width="450"></p><p align="center">The log browser enables detailed querying of moderation actions. Administrators can search through logs based on specific users, actions, or date ranges, providing detailed audit trails. No more sabotaging!</p> |
|                                                                                                                                                   Multi-format Translation                                                                                                                                                    |                                                                                                                                       Session State Preservation                                                                                                                                       |
|                      <p align="center"><img src="assets/images/05.gif" width="450"></p><p align="center">The review menu features translation capabilities, supporting natural languages, morse code, and binary. This ensures effective review of content across different languages and encodings.</p>                      |                                    <p align="center"><img src="assets/images/06.gif" width="450"></p><p align="center">Review sessions are preserved across channels and servers, allowing moderators to seamlessly resume their work from where they left off.</p>                                    |
|                                                                                                                                                   Live Statistics Dashboard                                                                                                                                                   |                                                                                                                                         Priority Queue System                                                                                                                                          |
|                                                   <p align="center"><img src="assets/images/07.gif" width="450"></p><p align="center">The dashboard displays live hourly statistics showing active reviewers and various statistics for real-time performance tracking.</p>                                                   |                        <p align="center"><img src="assets/images/08.gif" width="450"></p><p align="center">Users can be added to priority queues for immediate processing. Workers automatically process these queues to check for potential violations using AI analysis.</p>                         |
|                                                                                                                                             Real-time Worker Progress Monitoring                                                                                                                                              |                                                                                                                                                                                                                                                                                                        |
|                   <p align="center"><img src="assets/images/09.gif" width="450"></p><p align="center">Workers can be monitored in real-time to track progress directly from the terminal. Administrators can easily track the status and performance of various workers to ensure efficient operation.</p>                    |                                                                                                                                                                                                                                                                                                        |

## 🚀 Features

Rotector offers key features for efficient moderation:

- **AI-powered Analysis**

  - OpenAI integration for content analysis
  - High-accuracy detection of inappropriate profiles
  - Validation measures to prevent AI hallucination

- **Discord Review System**

  - Efficient review and action workflow
  - Overview of outfits, friends, groups
  - Streamer mode for content creators
  - Translation support (natural languages, morse code, binary)
  - Session state preservation
  - **Community Review Mode:**
    - Upvote/downvote system for flagged users
    - No external links or sensitive data exposed
    - Assists moderator decision-making
  - **Official Review Mode:**
    - Full uncensored view of user information
    - Ability to trigger AI worker rechecks
    - Access to detailed activity logs

- **Worker System**

  - Multi-threaded parallel processing
  - Flexible command-line interface for worker management
  - Worker types:
    - AI Workers: Processes affiliations of flagged users or users in flagged groups
    - Purge Workers: Maintains database hygiene for old data and users
    - Queue Workers: Processes the queue of users to be checked by AI workers
    - Stats Worker: Uploads cached statistics to PostgreSQL daily

- **Priority Queue System**

  - Custom user ID queueing with priority levels
  - Automated AI analysis of queued users

- **Optimized Requests** ([axonet](https://github.com/jaxron/axonet) & [RoAPI.go](https://github.com/jaxron/roapi.go))

  - Fault-tolerant operations with [circuit breaker patterns](https://en.wikipedia.org/wiki/Circuit_breaker_design_pattern)
  - Retry mechanisms with [exponential backoff](https://en.wikipedia.org/wiki/Exponential_backoff)
  - Request deduplication through [single flight](https://pkg.go.dev/golang.org/x/sync/singleflight)
  - Rotatable proxy support to avoid rate limits
  - Response caching to reduce load on Roblox API

- **Detailed Logging**
  - Log files for operations and errors
  - Full audit trails and statistics
  - Real-time monitoring dashboard

## 📦 Prerequisites

> [!WARNING]
> This tool requires significant resources and technical expertise to run properly. It is not recommended for casual users without the necessary infrastructure.

### Essential

- [Go](https://go.dev/) 1.23.2
- [PostgreSQL](https://www.postgresql.org/) 17.0 (with [TimescaleDB](https://www.timescale.com/) 2.17.1 extension)
- [DragonflyDB](https://dragonflydb.io/) 1.25.1 or [Redis](https://redis.io/) 7.4.1
- OpenAI API key (uses [gpt-4o-mini](https://platform.openai.com/docs/models/gpt-4o-mini#gpt-4o-mini))
- Discord Bot token

### Optional

- Proxies: For distributed requests and bypassing rate limits
- Cookies: Not necessary at this time

## 🎯 Accuracy

Rotector is designed to find inappropriate content while minimizing false flags. Here's how it works:

### Detection Process

Rotector uses two detection systems for users and groups:

#### User Detection

1. **Smart Scoring**:
   We look at different factors like friends and account details to identify inappropriate content. Our system is carefully tuned to catch both obvious and subtle patterns while avoiding false positives.

2. **AI Checks**:
   The AI uses a conservative approach - it only flags accounts when there is clear evidence of violations. While this means it might miss some borderline cases, it ensures high confidence in flagged accounts. The AI friend worker systematically analyzes friend lists of flagged accounts which has proven to be very effective.

3. **Validation Measures**:
   When the AI flags content, we always check to make sure that content really exists on the user's profile. This extra step helps us avoid mistakes and keeps our system reliable.

#### Group Detection

1. **Member Monitoring**:
   The system tracks the number of flagged users within each group. When the count of flagged members surpasses a threshold, the group is automatically flagged for review. This approach helps identify potentially inappropriate communities that may contain users engaging in inappropriate behavior.

2. **Whitelist System**:
   Popular communities such as official Roblox groups or major fan clubs may initially trigger flags due to their large user base. However, once these groups are manually reviewed and cleared by moderators, they are permanently added to a whitelist to prevent future flags. This whitelist status isn't permanent though as administrators are able to reverse this status.

> [!TIP]
> Interested in seeing how well it performs? Check out our test results in the [Efficiency](#-efficiency) section.

### What We Don't Detect

To avoid false positives, Rotector won't flag accounts for:

<details>
<summary>Just having certain friends or followers</summary>
We don't flag accounts just because they're connected to someone breaking the rules. They might have been added or followed without knowing the other person was breaking rules. However, if we find an account is part of a large network of bad accounts, we will flag it.
</details>

<details>
<summary>Normal friendship conversations</summary>
Regular social interactions and friendly relationships are normal on Roblox.
</details>

<details>
<summary>Regular emojis or internet slang</summary>
Many emojis and slang words can mean different things and are often used innocently. We look at how they're being used before making any decisions.
</details>

<details>
<summary>Art without inappropriate themes</summary>
Creative expression is important on Roblox. We only flag art that clearly breaks the rules.
</details>

<details>
<summary>Talking about gender or orientation</summary>
These are normal parts of personal identity. Flagging such content could unfairly target users just for being themselves.
</details>

<details>
<summary>Normal roleplay games</summary>
Roleplaying is a big part of Roblox. We only flag roleplay that's clearly inappropriate.
</details>

<details>
<summary>Regular bad language</summary>
Normal swearing and insults are handled by Roblox's chat filters. We focus on more serious safety issues.
</details>

## ⚡ Efficiency

Rotector is designed to be highly efficient in processing large volumes of data while maintaining reasonable resource usage. Below is a performance snapshot from one of our test runs (as of November 7, 2024).

> [!NOTE]
> These results are from a single test run and should be considered illustrative rather than definitive. Performance can vary significantly based on multiple factors like API response times, proxy performance, system resources, configuration, and more.

### Test Configuration

- Users to Scan: 500
- Workers: 3 AI friend workers
- Proxies: 250 Romanian Location
- Rate Limit: 200 requests/second

### Performance Metrics

| Metric                              | Value                 |
| ----------------------------------- | --------------------- |
| Time Taken                          | 44 minutes 32 seconds |
| **Accounts Flagged**                | **7,078**             |
| API Requests Sent                   | 74,243                |
| API Request Success Rate            | 99.86%                |
| Bandwidth Used                      | 753.48 MB             |
| OpenAI Token Cost                   | $0.16                 |
| OpenAI API Calls                    | 241                   |
| Redis Memory Usage                  | 803.92 MB             |
| Redis Key Count                     | 69,663                |
| Concurrent Goroutines (min/avg/max) | 335 / 2,428 / 4,454   |

> [!NOTE]
> At this rate, a 24-hour runtime would theoretically flag approximately **228,869 users** with AI costs of $5.17. However, the actual number of flagged users would likely decrease over time as more users are added to the database, since previously processed users would not need to be rechecked.

### Accuracy Validation

In a manual review of 100 randomly selected flagged users from this test run:

- 99 users were confirmed as correctly flagged
- 1 user was cleared due to insufficient profile information

## 🔄 Reviewing

Rotector has two different ways to review flagged accounts: one for community members and one for official moderators. This dual approach ensures both community engagement and high standards of moderation.

### Community Review Mode

Anyone can help review flagged accounts through a carefully designed training mode. To protect privacy, this mode censors user information and hides external links. Users can participate by upvoting/downvoting based on whether they think an account breaks the rules, which helps point out accounts that need urgent review.

This system helps official moderators in several ways:

- Finds the most serious cases quickly
- Gives moderators extra input for their decisions
- Helps train new moderators
- Lets the community help keep Roblox safe

### Official Review Mode

Official moderators have more tools and permissions for account review. They can:

- See all account information (unless they turn on streamer mode)
- Ask AI workers to recheck accounts
- See logs of all moderation actions
- Make changes to the database
- Switch between standard mode and training mode

What makes this mode special is that moderators can do everything needed to handle flagged accounts. While community votes help, moderators make the final decisions about what happens to flagged accounts.

This dual-system approach works well because it lets everyone help out while making sure trained moderators handle the final decisions.

## 🛣️ Roadmap

This roadmap shows our major upcoming features, but we've got even more in the works! We're always adding new features based on what the community suggests.

- 👥 **Moderation Tools**

  - [ ] Appeal process system
  - [ ] Inventory inspection

- 🔍 **Scanning Capabilities**

  - [ ] Group content detection (wall posts, names, descriptions)

- 🌐 **Public API** (Available in Beta)
  - [ ] REST API for developers to integrate with
  - [ ] Script for Roblox game developers to integrate with

## ❓ FAQ

<details>
<summary>How do I set this up myself?</summary>

Detailed setup instructions will be available during the beta phase when the codebase is more stable. During alpha, we're focusing on making frequent changes, which makes maintaining documentation difficult.

If you'd like to attempt setting up Rotector anyway, you can examine:

- Configuration files in [`/config`](/config)
- Worker implementation in [`/cmd/worker`](/cmd/worker)
- Bot implementation in [`/cmd/bot`](/cmd/bot)
- Docker setup in [`Dockerfile`](/Dockerfile)

Note that setting up Rotector requires:

1. **Technical Expertise**: Experience with Go, PostgreSQL, and distributed systems
2. **Infrastructure**: Proper server setup and resource management
3. **Maintenance Knowledge**: Understanding of the codebase to handle updates and issues

⚠️ We recommend waiting for the beta release for a better setup process with documentation.

</details>

<details>
<summary>What's the story behind Rotector?</summary>

Rotector started when [jaxron](https://github.com/jaxron) developed two important libraries on September 23, 2024: [RoAPI.go](https://github.com/jaxron/roapi.go) and [axonet](https://github.com/jaxron/axonet). These libraries became the backbone of Rotector's networking and API interaction capabilities.

Rotector's official development began secretly on October 13, 2024 due to his personal concerns about inappropriate behavior on Roblox and a desire to help protect young players. The project was made public for the alpha testing phase on November 8, 2024.

While Roblox already has moderators, there are so many users that it's hard to catch every inappropriate account quickly. Some Roblox staff have also acknowledged that it's difficult to handle all the reports they get. Sometimes, inappropriate accounts stay active even after being reported.

Rotector helps by finding these accounts automatically. Our goal is to make moderation easier and help keep the Roblox community, especially young players, safer.

</details>

<details>
<summary>Why is Rotector open-sourced?</summary>

We believe in transparency and the power of open source. By making our code public, anyone can understand how the tool works. It's also a great way for people to learn about online safety and moderation tools.

While we welcome feedback, ideas, and contributions, this open-source release is mainly to show how the tool works and help others learn from it.

</details>

<details>
<summary>When will the REST API be available?</summary>

The REST API will be ready during the beta phase. At this time, we're focusing on making sure the core features work well. Once those are stable, we'll work on the API so other developers can use Rotector in their own projects.

</details>

<details>
<summary>Can I use Rotector without the Discord bot?</summary>

Yes, but the Discord bot makes reviewing accounts much easier. The main features (finding and reporting inappropriate accounts) work fine without Discord. If you don't want to use the Discord bot, you'll need to create your own way to review the accounts that get flagged.

</details>

<details>
<summary>Why use Discord instead of a custom web interface?</summary>

Discord already has everything we need for reviewing accounts - buttons, dropdowns, forms, and rich embeds. Using Discord lets us focus on making Rotector better instead of building a whole new interface from scratch.

</details>

<details>
<summary>Are proxies and cookies necessary to use Rotector?</summary>

No, you don't need proxies or cookies to use Rotector. Proxies can help if you're making lots of requests, but they're optional. While cookies are mentioned in the settings, we don't use them for anything at the moment.

Proxies can be set up in the `config.toml` file. This file also includes a rate limit setting that lets you control how many requests Rotector makes to Roblox's API.

</details>

<details>
<summary>Will users who have stopped their inappropriate behavior be removed from the database?</summary>

No, past rule violations remain in the database, even if users say they've changed. This can be useful for law enforcement investigations and for future safety concerns. Some users try to clean up their profiles temporarily, only to return to breaking rules later.

This isn't about preventing second chances - it's about keeping the platform safe, especially for young users.

</details>

<details>
<summary>Who inspired the creation of Rotector?</summary>

[Ruben Sim](https://www.youtube.com/@RubenSim), a YouTuber and former game developer, helped inspire Rotector. His work exposing Roblox's moderation problems, especially through the [Moderation for Dummies](https://x.com/ModForDummies) Twitter account, showed what one person could do even without special tools. We are deeply grateful for his contributions which helped pave the way for our project.

</details>

<details>
<summary>How did "Rotector" get its name?</summary>

The name comes from three ideas:

1. **Protector**: We want to protect Roblox players from inappropriate content
2. **Detector**: We find inappropriate accounts
3. **"Ro-" prefix**: From "Roblox", the platform we work with

</details>

## 👥 Contributing

We follow the [Contributor Covenant](CODE_OF_CONDUCT.md) Code of Conduct. If you're interested in contributing to this project, please abide by its terms.

If you're feeling extra supportive, you can always buy us a coffee! ☕

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/K3K612AWHA)

## 📄 License

This project is licensed under the GNU General Public License v2.0 - see the [LICENSE](LICENSE) file for details.

## ⚠️ Disclaimer

Roblox is a registered trademark of Roblox Corporation. "Rotector" and the Rotector logo are not affiliated with, endorsed by, or sponsored by Roblox Corporation.

Rotector is free software: you can redistribute it under the terms of the GNU General Public License version 2 as published by the Free Software Foundation. You may modify the software for your own use. If you distribute modified versions, you must do so under the same GPL v2 license and make the source code of your modifications available.

### Terms of Service Compliance

While Rotector only accesses publicly available information through Roblox's API, users should be aware that:

- This tool should not be used to harass or target specific users
- Any automated scanning and excessive requests may violate Roblox's Terms of Service
- Users are responsible for respecting the rate limit
