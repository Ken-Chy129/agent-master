import type { InfoResponse } from './types.js';

/**
 * Whether a machine can actually run a session, and why not.
 *
 * "Reachable" and "usable" are different claims, and conflating them is how a
 * machine showed a green dot for a month while every message failed: the dot was
 * driven by "the /api/info request returned", which was always true. Reachability
 * belongs to the request; readiness belongs to this.
 */
export interface Readiness {
  /** false only when the daemon positively reports a blocking problem. */
  ready: boolean;
  /** Short label for a dot tooltip or badge. null when ready. */
  reason: string | null;
  /** Whether the daemon refuses sends outright, as opposed to letting them fail. */
  refusing: boolean;
}

const READY: Readiness = { ready: true, reason: null, refusing: false };

/**
 * Derive readiness from GET /api/info.
 *
 * Every field is treated as a tri-state: a daemon older than these fields omits
 * them, and "absent" must read as "cannot tell" rather than "broken". Reporting an
 * un-upgraded machine as unusable would be its own false alarm.
 */
export function machineReadiness(info: InfoResponse | null | undefined): Readiness {
  if (!info) return READY;

  // Refused sends first: it is both the more severe state and the cause of the
  // missing credential it would otherwise be reported as.
  const env = info.shell_env;
  if (env?.blocked === true) {
    const missing = env.missing?.length ? env.missing.join('、') : '登录 shell 凭证';
    return {
      ready: false,
      refusing: true,
      reason: `${missing} 已无法读取，会话请求会被拒绝`,
    };
  }

  if (info.auth?.ok === false) {
    return { ready: false, refusing: false, reason: '未找到 Claude 凭证，会话会失败' };
  }

  const claude = info.providers?.claude;
  if (claude && claude.available === false) {
    return { ready: false, refusing: false, reason: '这台机器上找不到 Claude CLI' };
  }

  return READY;
}
