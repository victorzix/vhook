const PREFIX = "whsec_";
const VISIBLE_TAIL = 4;

/**
 * Hides the middle of a signing secret. §4.28 allows revealing it as often as
 * you like, and the detail route returns it — but this dashboard gets shown to
 * people, and a secret on screen leaks into screenshots and screen shares.
 */
export function maskSecret(secret: string): string {
  const body = secret.startsWith(PREFIX) ? secret.slice(PREFIX.length) : secret;
  if (body.length <= VISIBLE_TAIL) {
    return `${PREFIX}${"•".repeat(8)}`;
  }
  return `${PREFIX}${"•".repeat(8)}${body.slice(-VISIBLE_TAIL)}`;
}
