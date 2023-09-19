module.exports = {
  theme: "cosmos",
  title: "IBC-Go",
  locales: {
    "/": {
      lang: "en-US"
    },
  },
  base: process.env.VUEPRESS_BASE || "/",
  head: [
    ['link', { rel: "apple-touch-icon", sizes: "180x180", href: "/apple-touch-icon.png" }],
    ['link', { rel: "icon", type: "image/png", sizes: "32x32", href: "/favicon-32x32.png" }],
    ['link', { rel: "icon", type: "image/png", sizes: "16x16", href: "/favicon-16x16.png" }],
    ['link', { rel: "manifest", href: "/site.webmanifest" }],
    ['meta', { name: "msapplication-TileColor", content: "#2e3148" }],
    ['meta', { name: "theme-color", content: "#ffffff" }],
    ['link', { rel: "icon", type: "image/svg+xml", href: "/favicon-svg.svg" }],
    ['link', { rel: "apple-touch-icon-precomposed", href: "/apple-touch-icon-precomposed.png" }],
  ],
  themeConfig: {
    repo: "cosmos/ibc-go",
    docsRepo: "cosmos/ibc-go",
    docsBranch: "main",
    docsDir: "docs",
    editLinks: true,
    label: "ibc",
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
      },
      {
        "label": "v1.1.0",
        "key": "v1.1.0"
      },
      {
        "label": "v1.2.0",
        "key": "v1.2.0"
      },
      {
        "label": "v1.3.0",
        "key": "v1.3.0"
      },
      {
        "label": "v1.4.0",
        "key": "v1.4.0"
      },
      {
        "label": "v1.5.0",
        "key": "v1.5.0"
      },
      {
        "label": "v2.0.0",
        "key": "v2.0.0"
      } ,
      {
        "label": "v2.1.0",
        "key": "v2.1.0"
      }, 
      { 
        "label": "v2.2.0",
        "key": "v2.2.0"
      },
      { 
        "label": "v2.3.0",
        "key": "v2.3.0"
      },
      {
        "label": "v3.0.0",
        "key": "v3.0.0"
      },
      {
        "label": "v3.1.0",
        "key": "v3.1.0"
      }
    ],
    topbar: {
      banner: true
    },
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
              title: "Middleware",
              directory: true,
              path: "/ibc/middleware"
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
            {
              title: "Roadmap",
              directory: false,
              path: "/roadmap/roadmap.html"
            },
          ]
        },
        {
          title: "IBC Application Modules",
          children: [
            {
              title: "Interchain Accounts",
              directory: true,
              path: "/apps",
              children: [
                {
                    title: "Overview",
                    directory: false,
                    path: "/apps/interchain-accounts/overview.html"
                }, 
                {
                  title: "Authentication Modules",
                  directory: false,
                  path: "/apps/interchain-accounts/auth-modules.html"
                },
                {
                  title: "Active Channels",
                  directory: false,
                  path: "/apps/interchain-accounts/active-channels.html"
                },
                {
                    title: "Integration",
                    directory: false,
                    path: "/apps/interchain-accounts/integration.html"
                },
                {
                  title: "Parameters",
                  directory: false,
                  path: "/apps/interchain-accounts/parameters.html"
                },
                {
                  title: "Transactions",
                  directory: false,
                  path: "/apps/interchain-accounts/transactions.html"
                },
            ]
            },
          ]
        },
        {
          title: "Migrations",
          children: [
            {
              title: "Support transfer of coins whose base denom contains slashes",
              directory: false,
              path: "/migrations/support-denoms-with-slashes.html"
            },
            {
              title: "SDK v0.43 to IBC-Go v1",
              directory: false,
              path: "/migrations/sdk-to-v1.html"
            },
            {
              title: "IBC-Go v1 to v2",
              directory: false,
              path: "/migrations/v1-to-v2.html"
            },
            {
              title: "IBC-Go v2 to v3",
              directory: false,
              path: "/migrations/v2-to-v3.html"
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
      question: {
        text: "Chat with IBC developers in <a href='https://discord.gg/W8trcGV' target='_blank'>Discord</a>."
      },
      textLink: {
        text: "ibcprotocol.org",
        url: "https://ibcprotocol.org"
      },
      services: [
        {
          service: "medium",
          url: "https://blog.cosmos.network/"
        },
        {
          service: "twitter",
          url: "https://twitter.com/cosmos"
        },
        {
          service: "linkedin",
          url: "https://www.linkedin.com/company/interchain-gmbh"
        },
        {
          service: "reddit",
          url: "https://reddit.com/r/cosmosnetwork"
        },
        {
          service: "telegram",
          url: "https://t.me/cosmosproject"
        },
        {
          service: "youtube",
          url: "https://www.youtube.com/c/CosmosProject"
        }
      ],
      smallprint:
        "The development of IBC-Go is led primarily by [Interchain GmbH](https://interchain.berlin/). Funding for this development comes primarily from the Interchain Foundation, a Swiss non-profit.",
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
