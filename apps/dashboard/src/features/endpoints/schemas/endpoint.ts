import { z } from "zod";

import type { components } from "@/lib/api-types";

/**
 * Validates user input, and does NOT redeclare the contract: the submit is
 * typed with the generated type below, so TypeScript complains if the two
 * drift apart.
 */
export const createEndpointSchema = z.object({
  // `z.url()` and not `z.string().url()`: the latter is deprecated in zod 4.
  // The https rule stays a refine so only it carries `urlMustBeHttps` — moving
  // it into `z.url({protocol})` would give malformed input that same message.
  url: z.url().refine((value) => value.startsWith("https://"), {
    // The key, not the sentence: the component translates it.
    message: "urlMustBeHttps",
  }),
});

export type CreateEndpointInput = z.infer<typeof createEndpointSchema>;

// Compile-time check that the form shape matches what the API accepts.
type ContractRequest = components["schemas"]["CreateEndpointRequest"];
const _shapeMatchesContract: ContractRequest = { url: "https://example.com" } satisfies CreateEndpointInput;
void _shapeMatchesContract;
