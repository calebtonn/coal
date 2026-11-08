<script lang="ts" module>
  import type { BadgeVariant } from '@davenicoll/web-framework';
  import type { Status } from './types';

  export const STATUS_VARIANT: Record<Status, BadgeVariant> = {
    active: 'success',
    paused: 'warning',
    done: 'neutral',
    abandoned: 'ghost',
  };
</script>

<script lang="ts">
  import { Badge } from '@davenicoll/web-framework';

  let { status }: { status: Status } = $props();
</script>

<Badge variant={STATUS_VARIANT[status] ?? 'ghost'} size="sm" data-status={status}>{status}</Badge>
