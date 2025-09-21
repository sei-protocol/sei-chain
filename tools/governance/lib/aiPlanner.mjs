import { formatUnits, parseUnits } from "./tokenMath.mjs";

function toNumber(value, fallback = null) {
  if (value === undefined || value === null) return fallback;
  const num = typeof value === "string" ? Number(value) : Number(value);
  if (!Number.isFinite(num)) return fallback;
  return num;
}

function toBoolean(value) {
  if (value === undefined || value === null) return false;
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (typeof value === "string") {
    return ["1", "true", "yes", "on"].includes(value.toLowerCase());
  }
  return false;
}

function parseAmount(value, decimals) {
  if (value === undefined || value === null) return null;
  if (typeof value === "bigint") return value;
  if (typeof value === "number") {
    return parseUnits(value.toString(), decimals);
  }
  if (typeof value === "string") {
    if (value.startsWith("0x")) {
      return BigInt(value);
    }
    return parseUnits(value, decimals);
  }
  throw new Error(`Unsupported amount value: ${value}`);
}

function computeBacklogDays(outstanding, baselinePerDay, decimals) {
  if (!baselinePerDay || baselinePerDay === 0n) return null;
  if (!outstanding || outstanding === 0n) return 0;
  const outstandingFloat = Number.parseFloat(formatUnits(outstanding, decimals));
  const baselineFloat = Number.parseFloat(formatUnits(baselinePerDay, decimals));
  if (!Number.isFinite(outstandingFloat) || !Number.isFinite(baselineFloat) || baselineFloat === 0) {
    return null;
  }
  return outstandingFloat / baselineFloat;
}

function formatPercent(value) {
  if (value === null || value === undefined) return "n/a";
  return `${(value * 100).toFixed(2)}%`;
}

function normaliseStream(stream, decimals, type) {
  if (!stream) return null;
  const baseline = stream.baselineEmissionPerDay !== undefined ? parseAmount(stream.baselineEmissionPerDay, decimals) : null;
  const seedOutstanding = stream.seedOutstanding !== undefined ? parseAmount(stream.seedOutstanding, decimals) : null;
  const maxBacklogDays = toNumber(stream.maxBacklogDays);
  return {
    type,
    baselinePerDay: baseline,
    maxBacklogDays: maxBacklogDays !== null ? maxBacklogDays : null,
    seedOutstanding,
    boostOnUnderUtilization: toBoolean(stream.boostOnUnderUtilization ?? stream.meta?.boostOnUnderUtilization),
    notes: stream.notes ?? null,
  };
}

function normalisePolicy(policy) {
  if (!policy) {
    return {
      currentUtilization: null,
      targetUtilization: null,
      governors: [],
      notes: null,
    };
  }
  const governors = Array.isArray(policy.governors)
    ? policy.governors.filter((addr) => typeof addr === "string" && addr.length > 0)
    : [];
  return {
    currentUtilization: toNumber(policy.currentUtilization),
    targetUtilization: toNumber(policy.targetUtilization),
    governors,
    notes: policy.notes ?? null,
  };
}

function normaliseMarket(market, decimals) {
  const address = typeof market.address === "string" ? market.address : null;
  const lower = address ? address.toLowerCase() : null;
  return {
    address,
    addressLc: lower,
    label: market.label ?? (address ? `Market ${address}` : "Unknown Market"),
    borrow: normaliseStream(market.borrow, decimals, "borrow"),
    supply: normaliseStream(market.supply, decimals, "supply"),
    policy: normalisePolicy(market.policy),
    tags: Array.isArray(market.tags) ? market.tags.filter((tag) => typeof tag === "string") : [],
  };
}

function normaliseScheduleEntry(entry, decimals) {
  if (!entry) return null;
  const days = toNumber(entry.days);
  if (!Number.isFinite(days) || days <= 0) {
    return null;
  }
  const emissionPerDay = entry.emissionPerDay !== undefined ? parseAmount(entry.emissionPerDay, decimals) : null;
  return {
    label: entry.label ?? "Epoch",
    days,
    emissionPerDay,
  };
}

export function normaliseConfig(raw = {}) {
  const decimals = raw.rewardToken?.decimals !== undefined ? Number(raw.rewardToken.decimals) : 18;
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 30) {
    throw new Error(`Invalid reward token decimals: ${raw.rewardToken?.decimals}`);
  }
  const markets = Array.isArray(raw.markets)
    ? raw.markets.map((market) => normaliseMarket(market, decimals))
    : [];
  const schedule = Array.isArray(raw.emissionSchedule)
    ? raw.emissionSchedule.map((entry) => normaliseScheduleEntry(entry, decimals)).filter(Boolean)
    : [];
  const accounts = Array.isArray(raw.accounts)
    ? raw.accounts.filter((addr) => typeof addr === "string" && addr.length > 0)
    : [];
  return {
    distributor: raw.distributor ?? null,
    rewardToken: {
      symbol: raw.rewardToken?.symbol ?? "TOKEN",
      decimals,
    },
    accounts,
    markets,
    emissionSchedule: schedule,
    network: raw.network ?? null,
    global: {
      bufferDays: toNumber(raw.global?.bufferDays),
      maxSystemBacklogDays: toNumber(raw.global?.maxSystemBacklogDays),
    },
  };
}

export function mergeOutstandingRewards(existingMap, rewards) {
  const map = new Map(existingMap ?? []);
  for (const reward of rewards) {
    const key = `${reward.tToken.toLowerCase()}:${reward.isBorrowReward ? "borrow" : "supply"}`;
    const entry = map.get(key) ?? { amount: 0n, tToken: reward.tToken, isBorrowReward: reward.isBorrowReward };
    entry.amount = entry.amount + BigInt(reward.amount);
    map.set(key, entry);
  }
  return map;
}

function determineSeverity(backlogDays, threshold, fallbackCritical) {
  if (backlogDays === null) return "unknown";
  if (backlogDays === 0) return "healthy";
  const limit = threshold ?? fallbackCritical;
  if (!limit) {
    if (backlogDays > 7) return "critical";
    if (backlogDays > 3) return "warning";
    return "healthy";
  }
  if (backlogDays >= limit * 1.5) return "critical";
  if (backlogDays > limit) return "warning";
  return "healthy";
}

function computeEmissionInsights(schedule, decimals, horizonDays, totalOutstanding, totalBaseline) {
  if (!schedule || schedule.length === 0) {
    return null;
  }
  let remaining = horizonDays;
  let cumulative = 0n;
  const breakdown = [];
  for (const entry of schedule) {
    if (remaining <= 0) break;
    const days = Math.min(remaining, entry.days ?? 0);
    const emissionPerDay = entry.emissionPerDay ?? 0n;
    const emitted = emissionPerDay * BigInt(days);
    cumulative += emitted;
    breakdown.push({
      label: entry.label,
      days,
      emissionPerDay: emissionPerDay.toString(),
      totalEmission: emitted.toString(),
    });
    remaining -= days;
  }
  const outstandingFloat = Number.parseFloat(formatUnits(totalOutstanding ?? 0n, decimals));
  const emissionFloat = Number.parseFloat(formatUnits(cumulative, decimals));
  const baselineFloat = totalBaseline && totalBaseline > 0n
    ? Number.parseFloat(formatUnits(totalBaseline, decimals))
    : null;
  const coverageDays = baselineFloat && baselineFloat > 0 ? emissionFloat / baselineFloat : null;
  return {
    projectedEmission: cumulative.toString(),
    projectedEmissionFormatted: emissionFloat.toFixed(6),
    outstandingCoverageRatio: emissionFloat > 0 ? outstandingFloat / emissionFloat : null,
    coverageDays,
    breakdown,
    shortfall: emissionFloat < outstandingFloat,
  };
}

export function buildGovernancePlan({ config, outstandingByStream, accounts, horizonDays = 30 }) {
  const plan = {
    generatedAt: new Date().toISOString(),
    distributor: config.distributor,
    rewardToken: config.rewardToken,
    network: config.network,
    accounts,
    summary: {
      totalOutstanding: { raw: "0", formatted: "0" },
      totalBaselinePerDay: { raw: "0", formatted: "0" },
      systemBacklogDays: null,
      systemSeverity: "unknown",
      emissionForecast: null,
    },
    markets: [],
    unknownStreams: [],
    globalRecommendations: [],
  };

  const decimals = config.rewardToken.decimals;
  const consumedKeys = new Set();
  let totalOutstanding = 0n;
  let totalBaseline = 0n;

  for (const market of config.markets) {
    const marketPlan = {
      address: market.address,
      label: market.label,
      tags: market.tags,
      policy: market.policy,
      streams: [],
    };

    for (const type of ["borrow", "supply"]) {
      const streamCfg = market[type];
      if (!streamCfg) continue;
      const key = `${market.addressLc ?? ""}:${type}`;
      const outstandingEntry = key.trim() ? outstandingByStream.get(key) : undefined;
      if (outstandingEntry) {
        consumedKeys.add(key);
      }
      const outstanding = outstandingEntry?.amount ?? streamCfg.seedOutstanding ?? 0n;
      const formattedOutstanding = formatUnits(outstanding, decimals);
      const backlogDays = computeBacklogDays(outstanding, streamCfg.baselinePerDay ?? 0n, decimals);
      const severity = determineSeverity(backlogDays, streamCfg.maxBacklogDays, config.global.maxSystemBacklogDays);

      totalOutstanding += outstanding;
      if (streamCfg.baselinePerDay) {
        totalBaseline += streamCfg.baselinePerDay;
      }

      const recommendations = [];
      if (backlogDays !== null && streamCfg.maxBacklogDays !== null && backlogDays > streamCfg.maxBacklogDays) {
        const method = type === "borrow"
          ? "updateMarketBorrowIndexAndDisburseBorrowerRewards"
          : "updateMarketSupplyIndexAndDisburseSupplierRewards";
        const severityLevel = backlogDays >= streamCfg.maxBacklogDays * 1.5 ? "critical" : "warning";
        const operations = accounts && accounts.length > 0
          ? accounts.map((account) => ({
              label: `${market.label} ${type} sync for ${account}`,
              type: "contractCall",
              contract: config.distributor,
              method,
              args: [market.address, account, true],
            }))
          : [];
        recommendations.push({
          severity: severityLevel,
          message: `Backlog of ${backlogDays.toFixed(2)} days exceeds threshold (${streamCfg.maxBacklogDays}). Trigger on-chain index sync and auto-claim loop.`,
          operations,
          governance: {
            type: "operational",
            description: `Authorize NodeBot to execute ${method} on ${market.label} (${type}) until backlog < ${streamCfg.maxBacklogDays} days.`,
          },
        });
      }

      if (
        streamCfg.boostOnUnderUtilization &&
        market.policy?.targetUtilization !== null &&
        market.policy?.currentUtilization !== null &&
        market.policy.targetUtilization > market.policy.currentUtilization
      ) {
        const delta = market.policy.targetUtilization - market.policy.currentUtilization;
        const severityLevel = delta > 0.1 ? "warning" : "info";
        recommendations.push({
          severity: severityLevel,
          message: `Utilization ${formatPercent(market.policy.currentUtilization)} below target ${formatPercent(market.policy.targetUtilization)}. Boost ${type} rewards to close the gap.`,
          governance: {
            type: "governanceProposal",
            title: `${market.label}: Adjust ${type} emission multiplier`,
            description: `Increase ${type} emission multiplier by ~${(delta * 100).toFixed(1)}% to steer utilization toward target.`,
            actions: [
              {
                type: "call",
                contract: config.distributor,
                method: "configureEmissionRate",
                args: [market.address, type, "<newEmissionRate>"],
                notes: "Replace <newEmissionRate> with calculated target before submission.",
              },
            ],
          },
        });
      }

      marketPlan.streams.push({
        type,
        outstanding: { raw: outstanding.toString(), formatted: formattedOutstanding },
        backlogDays,
        severity,
        recommendations,
        notes: streamCfg.notes,
      });
    }

    plan.markets.push(marketPlan);
  }

  for (const [key, entry] of outstandingByStream.entries()) {
    if (consumedKeys.has(key)) continue;
    plan.unknownStreams.push({
      key,
      tToken: entry.tToken,
      type: entry.isBorrowReward ? "borrow" : "supply",
      outstanding: {
        raw: entry.amount.toString(),
        formatted: formatUnits(entry.amount, decimals),
      },
    });
    totalOutstanding += entry.amount;
  }

  plan.summary.totalOutstanding = {
    raw: totalOutstanding.toString(),
    formatted: formatUnits(totalOutstanding, decimals),
  };
  plan.summary.totalBaselinePerDay = {
    raw: totalBaseline.toString(),
    formatted: formatUnits(totalBaseline, decimals),
  };
  plan.summary.systemBacklogDays = computeBacklogDays(totalOutstanding, totalBaseline, decimals);
  plan.summary.systemSeverity = determineSeverity(
    plan.summary.systemBacklogDays,
    config.global.maxSystemBacklogDays,
    config.global.maxSystemBacklogDays,
  );

  plan.summary.emissionForecast = computeEmissionInsights(
    config.emissionSchedule,
    decimals,
    horizonDays,
    totalOutstanding,
    totalBaseline,
  );

  if (
    config.global.maxSystemBacklogDays !== null &&
    plan.summary.systemBacklogDays !== null &&
    plan.summary.systemBacklogDays > config.global.maxSystemBacklogDays
  ) {
    plan.globalRecommendations.push({
      severity: "critical",
      message: `System backlog ${plan.summary.systemBacklogDays.toFixed(2)} days exceeds max ${config.global.maxSystemBacklogDays}. Escalate to emergency sync.`,
      governance: {
        type: "operational",
        description: "Engage NodeBot high-frequency mode and authorize additional relayers until backlog clears.",
      },
    });
  }

  if (plan.summary.emissionForecast?.shortfall) {
    plan.globalRecommendations.push({
      severity: "warning",
      message: "Projected emissions over the forecast horizon are insufficient to cover outstanding rewards.",
      governance: {
        type: "governanceProposal",
        title: "Top-up reward emission budget",
        description: "Submit proposal to replenish distributor reserves or adjust emission weights to avoid depletion.",
      },
    });
  }

  if (config.global.bufferDays && plan.summary.systemBacklogDays !== null && plan.summary.systemBacklogDays > config.global.bufferDays) {
    plan.globalRecommendations.push({
      severity: "warning",
      message: `Backlog exceeds configured buffer of ${config.global.bufferDays} days. Schedule governance review.`,
      governance: {
        type: "review",
        description: "Add agenda item for next governance call to review reward velocities and buffer targets.",
      },
    });
  }

  return plan;
}

function formatRecommendation(rec) {
  const parts = [`[${rec.severity.toUpperCase()}] ${rec.message}`];
  if (rec.operations && rec.operations.length > 0) {
    parts.push(`  • Operations: ${rec.operations.length} step(s).`);
  }
  if (rec.governance?.title) {
    parts.push(`  • Proposal: ${rec.governance.title}`);
  } else if (rec.governance?.description) {
    parts.push(`  • Governance: ${rec.governance.description}`);
  }
  return parts.join("\n");
}

export function formatPlanAsText(plan) {
  const lines = [];
  const symbol = plan.rewardToken.symbol ?? "TOKEN";
  lines.push(`AI Governance Plan for ${symbol} Emissions`);
  lines.push("=".repeat(lines[0].length));
  lines.push(`Generated: ${plan.generatedAt}`);
  if (plan.network?.name) {
    lines.push(`Network: ${plan.network.name}`);
  }
  if (plan.distributor) {
    lines.push(`Distributor: ${plan.distributor}`);
  }
  if (plan.accounts?.length) {
    lines.push(`Accounts analysed: ${plan.accounts.join(", ")}`);
  }
  lines.push("");
  lines.push(`Total outstanding: ${plan.summary.totalOutstanding.formatted} ${symbol}`);
  if (plan.summary.totalBaselinePerDay.raw !== "0") {
    lines.push(`Baseline emission / day: ${plan.summary.totalBaselinePerDay.formatted} ${symbol}`);
  }
  if (plan.summary.systemBacklogDays !== null) {
    lines.push(`System backlog: ${plan.summary.systemBacklogDays.toFixed(2)} days (${plan.summary.systemSeverity})`);
  }
  if (plan.summary.emissionForecast) {
    lines.push(
      `Projected emission (next window): ${plan.summary.emissionForecast.projectedEmissionFormatted} ${symbol} ` +
        `(shortfall: ${plan.summary.emissionForecast.shortfall ? "yes" : "no"})`,
    );
  }
  if (plan.globalRecommendations.length > 0) {
    lines.push("");
    lines.push("Global Recommendations:");
    for (const rec of plan.globalRecommendations) {
      lines.push(`- ${formatRecommendation(rec)}`);
    }
  }

  for (const market of plan.markets) {
    lines.push("");
    lines.push(`Market: ${market.label}${market.tags.length ? ` [${market.tags.join(", ")}]` : ""}`);
    if (market.address) {
      lines.push(`  Address: ${market.address}`);
    }
    if (market.policy?.currentUtilization !== null && market.policy?.targetUtilization !== null) {
      lines.push(
        `  Utilization: ${formatPercent(market.policy.currentUtilization)} (target ${formatPercent(market.policy.targetUtilization)})`,
      );
    }
    for (const stream of market.streams) {
      lines.push(`  Stream: ${stream.type}`);
      lines.push(`    Outstanding: ${stream.outstanding.formatted} ${symbol}`);
      if (stream.backlogDays !== null) {
        lines.push(`    Backlog: ${stream.backlogDays.toFixed(2)} days (${stream.severity})`);
      }
      if (stream.notes) {
        lines.push(`    Notes: ${stream.notes}`);
      }
      if (stream.recommendations.length === 0) {
        lines.push("    Recommendations: maintain current settings.");
      } else {
        lines.push("    Recommendations:");
        for (const rec of stream.recommendations) {
          const formatted = formatRecommendation(rec).split("\n");
          for (const line of formatted) {
            lines.push(`      - ${line}`);
          }
        }
      }
    }
  }

  if (plan.unknownStreams.length > 0) {
    lines.push("");
    lines.push("Unmapped reward streams detected:");
    for (const entry of plan.unknownStreams) {
      lines.push(
        `  - ${entry.key} :: ${entry.outstanding.formatted} ${symbol} outstanding (mark in config to include in planning)`,
      );
    }
  }

  return lines.join("\n");
}

export function formatPlanAsMarkdown(plan) {
  const symbol = plan.rewardToken.symbol ?? "TOKEN";
  const lines = [];
  lines.push(`# AI Governance Plan for ${symbol} Emissions`);
  lines.push(`_Generated: ${plan.generatedAt}_`);
  if (plan.network?.name) {
    lines.push(`- **Network:** ${plan.network.name}`);
  }
  if (plan.distributor) {
    lines.push(`- **Distributor:** \`${plan.distributor}\``);
  }
  if (plan.accounts?.length) {
    lines.push(`- **Accounts analysed:** ${plan.accounts.map((a) => `\`${a}\``).join(", ")}`);
  }
  lines.push(`- **Total outstanding:** ${plan.summary.totalOutstanding.formatted} ${symbol}`);
  if (plan.summary.totalBaselinePerDay.raw !== "0") {
    lines.push(`- **Baseline emission / day:** ${plan.summary.totalBaselinePerDay.formatted} ${symbol}`);
  }
  if (plan.summary.systemBacklogDays !== null) {
    lines.push(
      `- **System backlog:** ${plan.summary.systemBacklogDays.toFixed(2)} days (${plan.summary.systemSeverity})`,
    );
  }
  if (plan.summary.emissionForecast) {
    lines.push(
      `- **Projected emission (window):** ${plan.summary.emissionForecast.projectedEmissionFormatted} ${symbol} ` +
        `(shortfall: ${plan.summary.emissionForecast.shortfall ? "yes" : "no"})`,
    );
  }
  if (plan.globalRecommendations.length > 0) {
    lines.push("\n## Global Recommendations");
    for (const rec of plan.globalRecommendations) {
      lines.push(`- ${formatRecommendation(rec)}`);
    }
  }

  for (const market of plan.markets) {
    lines.push(`\n## ${market.label}`);
    if (market.address) {
      lines.push(`- Address: \`${market.address}\``);
    }
    if (market.policy?.currentUtilization !== null && market.policy?.targetUtilization !== null) {
      lines.push(
        `- Utilization: ${formatPercent(market.policy.currentUtilization)} (target ${formatPercent(market.policy.targetUtilization)})`,
      );
    }
    for (const stream of market.streams) {
      lines.push(`\n### ${stream.type.toUpperCase()} Stream`);
      lines.push(`- Outstanding: ${stream.outstanding.formatted} ${symbol}`);
      if (stream.backlogDays !== null) {
        lines.push(`- Backlog: ${stream.backlogDays.toFixed(2)} days (${stream.severity})`);
      }
      if (stream.notes) {
        lines.push(`- Notes: ${stream.notes}`);
      }
      if (stream.recommendations.length === 0) {
        lines.push("- Recommendations: Maintain current settings.");
      } else {
        lines.push("- Recommendations:");
        for (const rec of stream.recommendations) {
          lines.push(`  - ${formatRecommendation(rec)}`);
        }
      }
    }
  }

  if (plan.unknownStreams.length > 0) {
    lines.push("\n## Unknown Reward Streams");
    for (const entry of plan.unknownStreams) {
      lines.push(`- ${entry.key}: ${entry.outstanding.formatted} ${symbol}`);
    }
  }

  return lines.join("\n");
}
