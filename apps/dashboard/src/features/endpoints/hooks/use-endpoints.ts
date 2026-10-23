import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { createEndpoint, listEndpoints, updateEndpointUrl } from "../api/client";

const KEY = ["endpoints"] as const;

export function useEndpoints() {
  return useQuery({ queryKey: KEY, queryFn: listEndpoints });
}

export function useCreateEndpoint() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: createEndpoint,
    // Invalidate instead of writing into the cache by hand: the server is the
    // one truth, and apps/CLAUDE.md forbids a second copy of it.
    onSuccess: () => client.invalidateQueries({ queryKey: KEY }),
  });
}

export function useUpdateEndpointUrl() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, url }: { id: string; url: string }) => updateEndpointUrl(id, url),
    onSuccess: () => client.invalidateQueries({ queryKey: KEY }),
  });
}
