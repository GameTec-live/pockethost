import PocketBase from "pocketbase";

export const pb = new PocketBase(window.location.origin);
pb.autoCancellation(false);

export type Instance = {
  id: string;
  name: string;
  status: "running" | "stopped" | "deleted";
  port: number;
  created: string;
  updated: string;
};

export type OAuthProvider = {
  name: string;
  displayName: string;
};

export type LoginMethods = {
  password: boolean;
  providers: OAuthProvider[];
};

export async function firstRun(): Promise<{ firstRun: boolean }> {
  return pb.send("/api/pockethost/first-run", {});
}

export async function createFirstUser(input: { email: string; password: string }) {
  return pb.send("/api/pockethost/first-run", {
    method: "POST",
    body: input,
  });
}

export async function login(email: string, password: string) {
  return pb.collection("users").authWithPassword(email, password);
}

export async function listLoginMethods(): Promise<LoginMethods> {
  const methods = await pb.collection("users").listAuthMethods();
  return {
    password: methods.password.enabled,
    providers: methods.oauth2.enabled
      ? methods.oauth2.providers.map((provider) => ({
          name: provider.name,
          displayName: provider.displayName || provider.name,
        }))
      : [],
  };
}

export async function loginWithOAuth(provider: string) {
  return pb.collection("users").authWithOAuth2({ provider });
}

export async function listInstances(): Promise<Instance[]> {
  return pb.send("/api/pockethost/instances", {});
}

export async function createInstance(input: {
  name: string;
  superuserEmail: string;
  superuserPassword: string;
}): Promise<Instance> {
  return pb.send("/api/pockethost/instances", {
    method: "POST",
    body: input,
  });
}

export async function createInvite(input: { expiresHours: number }): Promise<{ id: string; url: string; expiresAt: string }> {
  return pb.send("/api/pockethost/invites", {
    method: "POST",
    body: input,
  });
}

export async function getInvite(token: string): Promise<{ valid: boolean; expiresAt: string }> {
  return pb.send(`/api/pockethost/invites/${token}`, {});
}

export async function acceptInvite(token: string, input: { email: string; password: string }) {
  return pb.send(`/api/pockethost/invites/${token}/accept`, {
    method: "POST",
    body: input,
  });
}

export async function renameInstance(id: string, name: string): Promise<Instance> {
  return pb.send(`/api/pockethost/instances/${id}`, {
    method: "PATCH",
    body: { name },
  });
}

export async function startInstance(id: string): Promise<Instance> {
  return pb.send(`/api/pockethost/instances/${id}/start`, { method: "POST" });
}

export async function stopInstance(id: string): Promise<Instance> {
  return pb.send(`/api/pockethost/instances/${id}/stop`, { method: "POST" });
}

export async function deployInstanceSite(id: string, file: File): Promise<Instance> {
  const body = new FormData();
  body.append("file", file);
  return pb.send(`/api/pockethost/instances/${id}/deploy`, {
    method: "POST",
    body,
  });
}

export async function deleteInstance(id: string): Promise<Instance> {
  return pb.send(`/api/pockethost/instances/${id}`, { method: "DELETE" });
}
