import { Show } from 'solid-js';
import cn from 'clsx';

import s from './AmberBusMark.module.pcss';
import type { AmberBusStatus } from './amberBus';

type AmberBusMarkProps = {
    status: AmberBusStatus | null;
};

/**
 * AmberBusMark shows that this AdGuardHome is the estate's fork, and whether
 * its Amber Bus connector is actually usable.
 *
 * The mark this replaces was static markup with title="Amber Bus connected",
 * rendered unconditionally. It said "connected" whether or not the connector
 * had a token, which meant it stayed reassuring during precisely the failure
 * it should have reported. This one renders nothing at all on a server that
 * does not publish the block, and says plainly when the connector is not
 * configured.
 */
export const AmberBusMark = (props: AmberBusMarkProps) => {
    const configured = () => props.status?.configured === true;

    const label = () => (configured() ? 'Amber Bus' : 'Amber Bus not configured');

    const title = () =>
        configured()
            ? `Amber Bus connector ready (${props.status?.mode ?? 'read-only'}) at ${props.status?.path ?? ''}`
            : `Amber Bus connector is not configured: ${'ADGUARDHOME_AMBER_BUS_TOKEN'} is unset, so bus calls are rejected`;

    return (
        <Show when={props.status}>
            <div
                class={cn(s.mark, { [s.unconfigured]: !configured() })}
                title={title()}
                aria-label={title()}
            >
                <svg class={s.icon} viewBox="0 0 24 24" role="img" aria-hidden="true" focusable="false">
                    <path d="M12 3.4 18.6 7v7.8L12 20.6l-6.6-5.8V7L12 3.4z" />
                    <path d="M8.4 9.4h7.2M8.4 12h7.2M8.4 14.6h7.2" />
                </svg>
                <span class={s.label}>{label()}</span>
            </div>
        </Show>
    );
};
