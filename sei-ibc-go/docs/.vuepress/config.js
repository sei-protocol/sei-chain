module.exports = {
  theme: "cosmos",
  title: "IBC-Go",
  locales: {
    "/": {
      lang: "en-US"
    },
  },
  base: process.env.VUEPRESS_BASE || "/",
  themeConfig: {
    repo: "cosmos/ibc-go",
    docsRepo: "cosmos/ibc-go",
    docsDir: "docs",
    editLinks: true,
    label: "ibc",
    //  label: "ibc-go",
    // TODO
    //algolia: {
    //  id: "BH4D9OD16A",
    //  key: "ac317234e6a42074175369b2f42e9754",
    //  index: "ibc-go"
    //},
    versions: [
      {
        "label": "main",
        "key": "main"
      }
    ],
    sidebar: { 
      auto: false,
      nav: [
          {
          title: "Using IBC-Go",
          children: [
            {
              title: "Overview",
              directory: false,
              path: "/ibc/overview.html"
            }, 
            {
              title: "Integration",
              directory: false,
              path: "/ibc/integration.html"
            },
            {
              title: "Applications",
              directory: false,
              path: "/ibc/apps.html"
            },
            {
              title: "Upgrades",
              directory: true,
              path: "/ibc/upgrades"
            },
            {
              title: "Governance Proposals",
              directory: false,
              path: "/ibc/proposals.html"
            },
            {
              title: "Relayer",
              directory: false,
              path: "/ibc/relayer.html"
            },
            {
              title: "Protobuf Documentation",
              directory: false,
              path: "/ibc/proto-docs.html"
            },
          ]
        },
        {
          title: "Migrations",
          children: [
            {
              title: "v0.43 SDK to IBC-Go v1.0.0",
              directory: false,
              path: "/migrations/ibc-migration-043.html"
            },
          ]
        },
        {
          title: "Resources",
          children: [
            {
              title: "IBC Specification",
              path: "https://github.com/cosmos/ibc"
            },
          ]
        }
      ]
    },
    gutter: {
      title: "Help & Support",
      editLink: true,
      chat: {
        title: "Discord",
        text: "Chat with IBC developers on Discord.",
        url: "https://discordapp.com/channels/669268347736686612",
        bg: "linear-gradient(225.11deg, #2E3148 0%, #161931 95.68%)"
      },
      github: {
        title: "Found an Issue?",
        text: "Help us improve this page by suggesting edits on GitHub."
      }
    },
    footer: {
      logo: "/logo-bw.svg",
      textLink: {
        text: "ibcprotocol.org",
        url: "https://ibcprotocol.org"
      },
      links: [
        {
          title: "Documentation",
          children: [
            {
              title: "Cosmos SDK",
              url: "https://docs.cosmos.network"
            },
            {
              title: "Cosmos Hub",
              url: "https://hub.cosmos.network"
            },
            {
              title: "Tendermint Core",
              url: "https://docs.tendermint.com"
            }
          ]
        },
        {
          title: "Community",
          children: [
            {
              title: "Cosmos blog",
              url: "https://blog.cosmos.network"
            },
            {
              title: "Forum",
              url: "https://forum.cosmos.network"
            },
            {
              title: "Chat",
              url: "https://discord.gg/W8trcGV"
            }
          ]
        },
        {
          title: "Contributing",
          children: [
            {
              title: "Contributing to the docs",
              url:
                "https://github.com/cosmos/ibc-go/blob/main/docs/DOCS_README.md"
            },
            {
              title: "Source code on GitHub",
              url: "https://github.com/cosmos/ibc-go/"
            }
          ]
        }
      ]
    }
  },
  plugins: [
    [
      "@vuepress/google-analytics",
      {
        ga: "UA-51029217-2"
      }
    ],
    [
      "sitemap",
      {
        hostname: "https://ibc.cosmos.network"
      }
    ]
  ]
};
