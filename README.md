<h1 align="center">
  <picture>
    <img width="350" alt="Rotector" src="./assets/images/rotector_logo.png">
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
</h1>

<p align="center">
  <em>When Roblox moderators dream of superpowers, they dream of <b>Rotector</b>. A powerful application written in <a href="https://go.dev/">Go</a>, designed to assist in identifying inappropriate user accounts on Roblox, with a focus on detecting ERP (Erotic Roleplay) accounts.</em>
</p>

---

> [!NOTE]
> This project is a community-driven initiative and is not affiliated with, endorsed by, or sponsored by Roblox Corporation. More details in the [Disclaimer](#%EF%B8%8F-disclaimer) section.

> [!WARNING]
> **A message to predators:** Rotector exists for one reason: to protect children from you. If you're on Roblox to exploit or harm kids, know that we will find you.

---

## 📚 Table of Contents

- [🚀 Features](#-features)
- [🧩 Components](#-components)
- [📦 Prerequisites](#-prerequisites)
- [🛠 Installation](#-installation)
- [🚀 Usage](#-usage)
- [❓ FAQ](#-faq)
- [🎯 Accuracy](#-accuracy)
- [🙏 Special Thanks](#-special-thanks)
- [📄 License](#-license)
- [⚠️ Disclaimer](#%EF%B8%8F-disclaimer)

## 🚀 Features

Rotector offers a comprehensive set of features for efficient moderation:

- **Recursive scanning** of Roblox user accounts for thorough investigation
- **AI-powered analysis** to detect inappropriate profiles with high accuracy
- **Multi-threaded processing** for high-performance parallel operations
- **Efficient storage** of flagged accounts using PostgreSQL database
- **Discord integration** enabling easy review and moderation of flagged accounts
- **Flexible command-line interface** to manage various types of workers
- **Robust logging system** for comprehensive operation tracking and debugging
- **Seamless Roblox API interaction** powered by [RoAPI.go](https://github.com/jaxron/roapi.go)
- **Enhanced network handling** utilizing [axonet](https://github.com/jaxron/axonet) for optimized requests

## 🧩 Components

Rotector consists of several key components:

1. **Workers**:
   - User Workers: Scan user friends and group members to identify potential ERP accounts.
     - Friend Worker: Processes friends of flagged users.
     - Group Worker: Processes members of flagged groups.
   - Group Workers: (Functionality not implemented yet)
2. **PostgreSQL**: Database to store and manage flagged user information.
3. **Redis Client**: Used for caching and managing distributed tasks.
4. **AI Processor**: Analyze user data to detect inappropriate content.
5. **Discord Bot**: Facilitates human review of flagged accounts.
6. **Logging System**: Tracks operations and assists in debugging.

## 📦 Prerequisites

Before installing Rotector, ensure you have the following:

### Essential

- Go 1.23.2+
- PostgreSQL 16+
- Redis 7+ or DragonflyDB 1.x
- OpenAI API key
- Discord Bot token

### Optional

- Proxies: For distributed requests
- Cookies: For authenticated Roblox API access

## 🛠 Installation

1. Clone the repository:

   ```bash
   git clone https://github.com/rotector/rotector.git
   cd rotector
   ```

2. Install dependencies:

   ```bash
   go mod tidy
   ```

3. Set up your configuration file (`config.toml`) with necessary API keys and database credentials.

4. Build the application:

   ```bash
   go build -o worker cmd/worker/main.go
   ```

   ```bash
   go build -o bot cmd/bot/main.go
   ```

## 🚀 Usage

Rotector uses a flexible CLI for running different types of workers. Here are some example commands:

1. Start user friend workers:

   ```bash
   ./worker user friend -w 3
   ```

   This command starts 3 user friend workers, which will process friends of flagged users.

2. Start user group workers:

   ```bash
   ./worker user group -w 5
   ```

   This command starts 5 user group workers, which will process members of flagged groups.

3. View available commands:

   ```bash
   ./worker --help
   ```

   This will display all available commands and options.

4. The workers will automatically start scanning Roblox accounts based on the configuration and previously flagged users/groups.

5. Start the Discord bot:

   ```bash
   ./bot
   ```

6. Use the `/review` command in Discord to review flagged accounts.

## 🎯 Accuracy

Rotector's AI processor is designed to identify potentially inappropriate content while minimizing false positives. Here's an overview of its capabilities.

### Detection Capabilities

Rotector flags a wide range of inappropriate content, including but not limited to sexual content, explicit language, and potential predatory behavior. The system is continuously refined to improve its accuracy in detecting nuanced and evolving forms of inappropriate content.

### What it Does Not Detect

To maintain focus and reduce false positives, Rotector is designed to avoid flagging:

- Users solely based on their friends list, followers, or following
- General mentions of friendship or relationships
- Non-sexual emojis or common internet slang
- Artistic content without explicit sexual themes
- Discussions about gender identity or sexual orientation
- References to non-sexual role-playing games
- General profanity or non-sexual insults

It's important to note that all flagged accounts have to undergo human review. This ensures that context and nuance are considered in the final decision-making process.

## ❓ FAQ

<details>
<summary>Why did you create Rotector?</summary>

We decided to create Rotector due to concerns about inappropriate behavior and content on the Roblox platform and we want to shield younger players from possible predators. Despite Roblox having its own moderation system, its large user base makes it difficult to swiftly detect and handle every inappropriate account. Inappropriate accounts—including ones that might belong to predators—have frequently been active for an extended amount of time despite reports.

By automating the initial detection, Rotector aims to simplify this process and enable quicker identification of potentially dangerous accounts. We want to contribute to the current moderating efforts and give the Roblox community—especially its younger members—an extra level of security.
</details>

<details>
<summary>Why is Rotector open-sourced?</summary>

We decided to release Rotector under an open source license because we think it is everyone's duty to keep children safe online. We're encouraging the whole community to contribute, enhance, and modify the tool to address the issues of online safety by making our code freely available.

Open-sourcing allows for:

1. Transparency: Our code is open for anybody to examine, ensuring that our practices are ethical and compliant with regional standards.
2. Cooperation: By pooling the knowledge of developers, security specialists, and child safety advocates, Rotector can become more efficient.
3. Education: For anyone who are interested in learning about online safety and moderation technologies, the project offers educational materials.
4. Adaptability: The open-source design enables rapid upgrades and enhancements from a worldwide community of contributors as threats change.

We believe that to truly combat the issue of inappropriate content and behavior on platforms like Roblox, we need everyone's involvement. It's not simply about one product or one team; it's about fostering a community-wide effort to create safer online spaces for children.
</details>

<details>
<summary>Are you getting paid and is Roblox sponsoring the project?</summary>

No, Rotector is an independent, community-driven project. Roblox is not paying us for this work, nor is it in any way formally associated with or sponsoring the project. The goal of this volunteer work is to support the Roblox community in keeping the community safe for all users, especially younger players.
</details>

<details>
<summary>How is the detection process like?</summary>

Rotector uses a multi-step process to detect potentially inappropriate accounts. There are multiple workers that are responsible for different tasks. For this example, we will focus on the user friend worker.

1. **Initial Flagging**: The system starts with known flagged users or groups, either manually input or identified through previous scans.

2. **Recursive Scanning**: For each flagged user, it fetches their friends list.

3. **Data Collection**: For each fetched user from the friends list, Rotector collects necessary data to perform an accurate analysis.

4. **AI Analysis**: The collected data is processed by an AI model (GPT-4o Mini) trained to identify patterns indicative of inappropriate content or behavior.

5. **Confidence Scoring**: The AI assigns a confidence score to each analyzed account, indicating the likelihood of it being inappropriate.

6. **Database Storage**: Accounts flagged by the AI are stored in a PostgreSQL database for further review.

7. **Human Review**: Through a Discord bot interface, human moderators can review the flagged accounts, confirming or dismissing the AI's findings.

This multi-layered approach allows Rotector to cast a wide net while using AI to filter out likely false positives, ultimately requiring human judgment for final decisions.
</details>

<details>
<summary>Can I use Rotector without the Discord bot?</summary>

Although the Discord bot is essential for the easy review and moderation of detected accounts, Rotector's primary features (identifying and reporting accounts with workers) can function on their own. If you decide not to use the Discord bot, you would have to implement another method for reviewing detected accounts. This could involve directly querying the database or creating a custom interface for reviewing detected accounts.
</details>

<details>
<summary>Are proxies and cookies necessary to use Rotector?</summary>

No, cookies and proxies are not required. However, they can be helpful for distributed requests and authorized Roblox API access. Cookies can give access to more specific account information, while proxies can help avoid IP blocks when making many requests.

You can configure these in the `config.toml` file if needed. This configuration file also includes a rate limiter setting, which allows you to control the frequency of requests to the Roblox API.
</details>

<details>
<summary>How did "Rotector" get its name?</summary>

The name "Rotector" is a combination of two words: "protector" and "detector", which reflects the purpose of the tool:

1. **Protector**: Aims to protect the Roblox community, especially younger players, from inappropriate content and potential predators.
2. **Detector**: Designed to detect and identify potentially inappropriate accounts on the platform.

Additionally, the "Ro-" prefix is a nod to "Roblox", the platform it's designed to work with. This blend of meanings represents the tool's primary function: to detect threats and protect users within the Roblox ecosystem.
</details>

## 🙏 Special Thanks

If you like this project, you'll appreciate the work others have done as well. Here are some community-initiated projects combating similar issues:

- **Ruben Sim**: A [YouTuber](https://www.youtube.com/@RubenSim) and former game developer known for his critiques and exposés of Roblox's moderation policies and issues. He is also known for running the Twitter account ["Moderation for Dummies"](https://x.com/ModForDummies), which consistently posts verified ERP accounts. This effort is really impressive given the lack of technical resources, funding, or official support, which shows the impact that dedicated individuals can have in addressing platform safety concerns.

- **UhTrue**: Creator of [Searcher](https://searcher.uhtrue.com/), a website designed to identify potentially inappropriate accounts on Roblox. The site analyzes user connections, including friendships and follower relationships, to flag accounts. This is another way community members are addressing platform safety. For more information and updates about the project, you may follow UhTrue on [Twitter](https://x.com/UhTrue_).

These projects and individuals were more than simply inspirations; they were the catalyst for Rotector's creation. We are deeply appreciative for their contributions, which created the groundwork for our project and continue to contribute our objective of improving safety in the Roblox community.

## 📄 License

This project is licensed under the GNU General Public License v2.0 - see the [LICENSE](LICENSE) file for details.

## ⚠️ Disclaimer

Roblox is a registered trademark of Roblox Corporation. This tool is provided as-is, without any guarantees or warranty. The authors are not responsible for any damage or data loss incurred with its use. Users should use this software at their own risk and ensure they comply with all applicable terms of service and legal requirements.

Rotector is designed to process only publicly available information from Roblox profiles. Users of the tool are responsible for implementing appropriate data retention and deletion policies in accordance with applicable regulatory requirements, such as CCPA or GDPR. While Rotector is designed with good intentions, it's important to understand that making excessive requests to Roblox's platform may violate their terms of service.

For more details, please refer to the [LICENSE](LICENSE) file.
