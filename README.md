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

> [!IMPORTANT]
> Roblox is a registered trademark of Roblox Corporation. This project is a community-driven initiative and is not affiliated with, endorsed by, or sponsored by Roblox Corporation. This tool is provided as-is, without any guarantees or warranty. The authors are not responsible for any damage or data loss incurred with its use. Users should use this software at their own risk and ensure they comply with all applicable terms of service and legal requirements. More details in the [LICENSE](LICENSE).

---

## 📚 Table of Contents

- [🚀 Features](#-features)
- [🧩 Components](#-components)
- [📦 Prerequisites](#-prerequisites)
- [🛠 Installation](#-installation)
- [🚀 Usage](#-usage)
- [❓ FAQ](#-faq)
- [🙏 Special Thanks](#-special-thanks)
- [📄 License](#-license)

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

## 🚀 Usage

Rotector uses a flexible CLI for running different types of workers. Here are some example commands:

1. Start user friend workers:

   ```bash
   ./worker user friend -w 3
   ```

   This command starts 3 user friend workers.

2. Start user group workers:

   ```bash
   ./worker user group -w 5
   ```

   This command starts 5 user group workers.

3. View available commands:

   ```bash
   ./worker --help
   ```

   This will display all available commands and options.

4. The workers will automatically start scanning Roblox accounts based on the configuration.

5. Use the Discord bot commands to review and moderate flagged accounts.

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
<summary>How does Rotector detect inappropriate accounts?</summary>

Rotector does recursive scanning of user accounts combined with AI-driven content analysis. It analyzes user data—such as friend lists, group memberships, and profile information—to spot patterns and content that can point to inappropriate activity.
</details>

<details>
<summary>How accurate is Rotector in detecting inappropriate accounts?</summary>

Rotector's AI-powered analysis makes use of GPT 4o Mini, which offers a high level of accuracy in identifying possibly inappropriate accounts. The accuracy depends on various factors, such as the quality of the training set and the particular patterns intended for identification. Although this automatic approach can be highly effective in identifying accounts that are inappropriate, it is not flawless. False positives can occur, which is why human review of detected accounts is a crucial part of the process. To improve accuracy, we are always updating and fine-tuning the AI model.
</details>

<details>
<summary>Can I use Rotector without the Discord bot?</summary>

Although the Discord bot is essential for the easy review and moderation of detected accounts, Rotector's primary features (identifying and reporting accounts with workers) can function on their own. If you decide not to use the Discord bot, you would have to implement another method for reviewing detected accounts. This could involve directly querying the database or creating a custom interface for reviewing detected accounts.
</details>

<details>
<summary>Are proxies and cookies necessary to use Rotector?</summary>

No, cookies and proxies are not required. However, they can be helpful for distributed requests and authorized Roblox API access. Cookies can give access to more specific account information, while proxies can help avoid IP blocks when making many requests. You can configure these in the `config.toml` file if needed, but Rotector will function without them, albeit potentially with some limitations.
</details>

<details>
<summary>Is Rotector legal and compliant with Roblox's terms of service?</summary>

Rotector is designed to process only publicly available information from Roblox profiles. Since the information gathered is already available to the general public, the program does not encrypt the data it stores. Nonetheless, Rotector users need to understand their obligations with regard to data handling and security. This means following the relevant regulations and laws, such as the CCPA or GDPR, which may call for the removal of data upon request or after a predetermined amount of time.

Users of the tool are responsible for putting in place suitable data retention and deletion policies in accordance with any applicable regulatory requirements. While Rotector is designed with good intentions, it's also important to understand that making too much requests to Roblox's platform can be against their terms of service.
</details>

## 🙏 Special Thanks

If you like this project, you'll appreciate the work others have done as well. Here are some community-initiated projects combating similar issues:

- **Ruben Sim**: A [YouTuber](https://www.youtube.com/@RubenSim) and former game developer known for his critiques and exposés of Roblox's moderation policies and issues. He is also known for running the Twitter account ["Moderation for Dummies"](https://x.com/ModForDummies), which consistently posts verified ERP accounts. This effort is really impressive given the lack of technical resources, funding, or official support, which shows the impact that dedicated individuals can have in addressing platform safety concerns.

- **UhTrue**: Creator of [Searcher](https://searcher.uhtrue.com/), a website designed to identify potentially inappropriate accounts on Roblox. The site analyzes user connections, including friendships and follower relationships, to flag accounts. This is another way community members are addressing platform safety. For more information and updates about the project, you may follow UhTrue on [Twitter](https://x.com/UhTrue_).

These projects and individuals were more than simply inspirations; they were the catalyst for Rotector's creation. We are deeply appreciative for their contributions, which created the groundwork for our project and continue to contribute our objective of improving safety in the Roblox community.

## 📄 License

This project is licensed under the GNU General Public License v2.0 - see the [LICENSE](LICENSE) file for details.
