import type { ServerStatus } from 'panel/api/model/serverStatus';

/**
 * AmberBusStatus is the fork-local `amber_bus` block that this AdGuardHome
 * fork adds to GET /control/status.
 *
 * It is declared here rather than in api/model/, because everything under
 * api/ is generated from openapi.yaml and a regeneration would silently drop
 * an edit made there. Keeping the fork's own shape beside its only consumer
 * survives that, and keeps the amber-bus surface narrow, which is what
 * AGENTS.md asks for.
 */
export type AmberBusStatus = {
    /** The connector's invoke endpoint, so the UI need not hard-code it. */
    path: string;

    /** The connector's exposure. Read-only in the first slice. */
    mode: string;

    /**
     * Whether the connector token is set on the server. When it is not, bus
     * callers fall through to session auth and the connector is unreachable
     * without anything saying so — which is the state the mark exists to
     * show.
     */
    configured: boolean;
};

/**
 * amberBusFromStatus reads the fork's block off a status response.
 *
 * The response is typed by generated code that does not know about this
 * field, so the read is narrowed here in one place instead of casting at
 * every call site. An older server, or an upstream build, simply has no
 * block and gets null — the mark then renders nothing at all rather than
 * guessing.
 */
export const amberBusFromStatus = (status: ServerStatus): AmberBusStatus | null => {
    const block = (status as ServerStatus & { amber_bus?: AmberBusStatus }).amber_bus;

    if (!block || typeof block.configured !== 'boolean') {
        return null;
    }

    return block;
};
