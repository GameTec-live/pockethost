import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, KeyRound, LogOut, Play, Plus, RefreshCw, Send, Settings, Square, Trash2, Upload } from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  acceptInvite,
  createInvite,
  createFirstUser,
  createInstance,
  deleteInstance,
  deployInstanceSite,
  firstRun,
  getInvite,
  Instance,
  listLoginMethods,
  login,
  loginWithOAuth,
  listInstances,
  pb,
  renameInstance,
  startInstance,
  stopInstance,
} from "./lib/pb";
import { Badge, Button, Field, Input, Panel } from "./components/ui";

const maxZipUploadBytes = 100 * 1024 * 1024;

export function App() {
  const [authVersion, setAuthVersion] = useState(0);
  const [authReady, setAuthReady] = useState(false);
  const firstRunQuery = useQuery({ queryKey: ["first-run"], queryFn: firstRun });
  const authed = pb.authStore.isValid;
  const inviteToken = new URLSearchParams(window.location.search).get("invite");

  useEffect(() => {
    let cancelled = false;
    async function refreshAuth() {
      if (!pb.authStore.isValid) {
        if (!cancelled) setAuthReady(true);
        return;
      }
      if (pb.authStore.record?.collectionName !== "users") {
        pb.authStore.clear();
        if (!cancelled) setAuthReady(true);
        return;
      }
      try {
        await pb.collection("users").authRefresh();
      } catch {
        pb.authStore.clear();
      } finally {
        if (!cancelled) setAuthReady(true);
      }
    }
    setAuthReady(false);
    refreshAuth();
    return () => {
      cancelled = true;
    };
  }, [authVersion]);

  if (firstRunQuery.isLoading || !authReady) {
    return <Shell centered>Loading</Shell>;
  }

  if (firstRunQuery.data?.firstRun) {
    return (
      <FirstRun
        onDone={() => {
          firstRunQuery.refetch();
          setAuthVersion((v) => v + 1);
        }}
      />
    );
  }

  if (inviteToken) {
    return <AcceptInvite token={inviteToken} onDone={() => setAuthVersion((v) => v + 1)} />;
  }

  if (!authed) {
    return <Login onDone={() => setAuthVersion((v) => v + 1)} />;
  }

  return <Dashboard key={authVersion} onLogout={() => setAuthVersion((v) => v + 1)} />;
}

function Shell({ children, centered = false }: { children: React.ReactNode; centered?: boolean }) {
  return (
    <main className={centered ? "grid min-h-screen place-items-center p-4" : "min-h-screen p-4 sm:p-6"}>
      <div className={centered ? "w-full max-w-md" : "mx-auto w-full max-w-6xl"}>{children}</div>
    </main>
  );
}

function FirstRun({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const mutation = useMutation({
    mutationFn: async () => {
      await createFirstUser({ email, password });
      return login(email, password);
    },
    onSuccess: onDone,
  });
  return (
    <Shell centered>
      <Panel>
        <h1 className="text-xl font-semibold">Create master admin</h1>
        <form className="mt-5 grid gap-4" onSubmit={(e) => submit(e, mutation.mutate)}>
          <Field label="Email">
            <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" required />
          </Field>
          <Field label="Password">
            <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" minLength={8} required />
          </Field>
          <ApiError error={mutation.error} />
          <Button loading={mutation.isPending}>Create admin</Button>
        </form>
      </Panel>
    </Shell>
  );
}

function Login({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const methods = useQuery({ queryKey: ["login-methods"], queryFn: listLoginMethods });
  const mutation = useMutation({
    mutationFn: () => login(email, password),
    onSuccess: onDone,
  });
  const oauth = useMutation({
    mutationFn: (provider: string) => loginWithOAuth(provider),
    onSuccess: onDone,
  });
  return (
    <Shell centered>
      <Panel>
        <h1 className="text-xl font-semibold">Pockethost</h1>
        {methods.data?.password ? (
          <form className="mt-5 grid gap-4" onSubmit={(e) => submit(e, mutation.mutate)}>
            <Field label="Email">
              <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" required />
            </Field>
            <Field label="Password">
              <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" required />
            </Field>
            <ApiError error={mutation.error} />
            <Button loading={mutation.isPending}>Sign in</Button>
          </form>
        ) : null}
        {methods.data && methods.data.providers.length > 0 ? (
          <div className={methods.data.password ? "mt-4 grid gap-2 border-t border-zinc-200 pt-4" : "mt-5 grid gap-2"}>
            {methods.data.providers.map((provider) => (
              <Button key={provider.name} variant="secondary" loading={oauth.isPending} onClick={() => oauth.mutate(provider.name)}>
                <KeyRound className="h-4 w-4" />
                {provider.displayName}
              </Button>
            ))}
            <ApiError error={oauth.error} />
          </div>
        ) : null}
        {methods.data && !methods.data.password && methods.data.providers.length === 0 ? <p className="mt-5 text-sm text-zinc-500">No login methods are enabled.</p> : null}
        <ApiError error={methods.error} />
      </Panel>
    </Shell>
  );
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const qc = useQueryClient();
  const instances = useQuery({ queryKey: ["instances"], queryFn: listInstances });
  const methods = useQuery({ queryKey: ["login-methods"], queryFn: listLoginMethods });
  const [createOpen, setCreateOpen] = useState(false);
  const [inviteOpen, setInviteOpen] = useState(false);
  const baseDomain = useMemo(() => window.location.host, []);
  const admin = isAdmin();

  return (
    <Shell>
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg bg-zinc-950 text-white">
            <Database className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold">Pockethost</h1>
            <p className="text-sm text-zinc-500">{pb.authStore.record?.email}</p>
          </div>
        </div>
        <div className="flex gap-2">
          {admin ? (
            <Button variant="secondary" size="icon" onClick={() => window.open("/_", "_blank", "noopener,noreferrer")}>
              <Settings className="h-4 w-4" />
            </Button>
          ) : null}
          <Button variant="secondary" size="icon" onClick={() => instances.refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          {admin && methods.data?.password && methods.data.providers.length === 0 ? (
            <Button variant="secondary" onClick={() => setInviteOpen(true)}>
              <Send className="h-4 w-4" />
              Invite
            </Button>
          ) : null}
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" />
            New DB
          </Button>
          <Button
            variant="secondary"
            size="icon"
            onClick={() => {
              pb.authStore.clear();
              onLogout();
            }}
          >
            <LogOut className="h-4 w-4" />
          </Button>
        </div>
      </header>

      {inviteOpen ? <InvitePanel onClose={() => setInviteOpen(false)} /> : null}
      {createOpen ? <CreatePanel onClose={() => setCreateOpen(false)} /> : null}

      <div className="grid gap-3">
        {instances.data?.map((instance) => (
          <InstanceRow key={instance.id} instance={instance} baseDomain={baseDomain} onChanged={() => qc.invalidateQueries({ queryKey: ["instances"] })} />
        ))}
        {instances.data?.length === 0 ? <Panel>No databases yet.</Panel> : null}
        <ApiError error={instances.error} />
      </div>
    </Shell>
  );
}

function isAdmin() {
  const record = pb.authStore.record;
  return record?.collectionName === "_superusers" || record?.role === "admin";
}

function InvitePanel({ onClose }: { onClose: () => void }) {
  const [expiresHours, setExpiresHours] = useState(72);
  const [inviteUrl, setInviteUrl] = useState("");
  const mutation = useMutation({
    mutationFn: () => createInvite({ expiresHours }),
    onSuccess: (invite) => setInviteUrl(invite.url),
  });
  return (
    <Panel className="mb-5">
      <form className="grid gap-4 md:grid-cols-[180px_auto]" onSubmit={(e) => submit(e, mutation.mutate)}>
        <Field label="Expires in hours">
          <Input value={expiresHours} onChange={(e) => setExpiresHours(Number(e.target.value))} type="number" min={1} max={720} required />
        </Field>
        <div className="flex items-end gap-2">
          <Button loading={mutation.isPending}>Create link</Button>
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </form>
      {inviteUrl ? (
        <div className="mt-4 flex flex-wrap gap-2">
          <Input value={inviteUrl} readOnly className="min-w-0 flex-1" />
          <Button variant="secondary" onClick={() => navigator.clipboard.writeText(inviteUrl)}>
            Copy
          </Button>
        </div>
      ) : null}
      <ApiError error={mutation.error} />
    </Panel>
  );
}

function AcceptInvite({ token, onDone }: { token: string; onDone: () => void }) {
  const invite = useQuery({ queryKey: ["invite", token], queryFn: () => getInvite(token) });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const mutation = useMutation({
    mutationFn: () => acceptInvite(token, { email, password }),
    onSuccess: async () => {
      await login(email, password);
      window.history.replaceState({}, "", "/");
      onDone();
    },
  });
  return (
    <Shell centered>
      <Panel>
        <h1 className="text-xl font-semibold">Accept invite</h1>
        {invite.isLoading ? <p className="mt-5 text-sm text-zinc-500">Loading invite</p> : null}
        {invite.data ? (
          <form className="mt-5 grid gap-4" onSubmit={(e) => submit(e, mutation.mutate)}>
            <Field label="Email">
              <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" required />
            </Field>
            <Field label="Password">
              <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" minLength={8} required />
            </Field>
            <ApiError error={mutation.error} />
            <Button loading={mutation.isPending}>Create account</Button>
          </form>
        ) : null}
        <ApiError error={invite.error} />
      </Panel>
    </Shell>
  );
}

function CreatePanel({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [superuserEmail, setEmail] = useState("");
  const [superuserPassword, setPassword] = useState("");
  const mutation = useMutation({
    mutationFn: () => createInstance({ name, superuserEmail, superuserPassword }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["instances"] });
      onClose();
    },
  });
  return (
    <Panel className="mb-5">
      <form className="grid gap-4 md:grid-cols-4" onSubmit={(e) => submit(e, mutation.mutate)}>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="demo" required />
        </Field>
        <Field label="Superuser email">
          <Input value={superuserEmail} onChange={(e) => setEmail(e.target.value)} type="email" required />
        </Field>
        <Field label="Superuser password">
          <Input value={superuserPassword} onChange={(e) => setPassword(e.target.value)} type="password" minLength={8} required />
        </Field>
        <div className="flex items-end gap-2">
          <Button loading={mutation.isPending}>Create</Button>
          <Button type="button" className="bg-white text-zinc-950" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </form>
      <ApiError error={mutation.error} />
    </Panel>
  );
}

function InstanceRow({ instance, baseDomain, onChanged }: { instance: Instance; baseDomain: string; onChanged: () => void }) {
  const [name, setName] = useState(instance.name);
  const [zipFile, setZipFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState("");
  const rename = useMutation({ mutationFn: () => renameInstance(instance.id, name), onSuccess: onChanged });
  const start = useMutation({ mutationFn: () => startInstance(instance.id), onSuccess: onChanged });
  const stop = useMutation({ mutationFn: () => stopInstance(instance.id), onSuccess: onChanged });
  const deploy = useMutation({
    mutationFn: () => {
      if (!zipFile) throw new Error("Choose a zip file first.");
      if (zipFile.size > maxZipUploadBytes) throw new Error("Zip file cannot exceed 100 MB.");
      return deployInstanceSite(instance.id, zipFile);
    },
    onSuccess: () => {
      setZipFile(null);
      onChanged();
    },
  });
  const del = useMutation({ mutationFn: () => deleteInstance(instance.id), onSuccess: onChanged });
  const host = `${instance.name}.${baseDomain}`;
  const busy = rename.isPending || start.isPending || stop.isPending || deploy.isPending || del.isPending;
  const confirmDelete = () => {
    if (window.confirm(`Delete ${instance.name} and its tenant data? This cannot be undone.`)) {
      del.mutate();
    }
  };
  const selectZip = (file: File | undefined) => {
    setFileError("");
    setZipFile(null);
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".zip")) {
      setFileError("Choose a .zip file.");
      return;
    }
    if (file.size > maxZipUploadBytes) {
      setFileError("Zip file cannot exceed 100 MB.");
      return;
    }
    setZipFile(file);
  };
  return (
    <Panel className="grid gap-4 md:grid-cols-[1fr_auto] md:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="truncate text-lg font-semibold">{instance.name}</h2>
          <Badge tone={instance.status === "running" ? "good" : "warn"}>{instance.status}</Badge>
        </div>
        <a className="mt-1 block truncate text-sm text-zinc-500 hover:text-zinc-950" href={`//${host}`} target="_blank">
          {host}
        </a>
      </div>
      <div className="flex flex-wrap gap-2">
        <Input className="w-40" value={name} onChange={(e) => setName(e.target.value)} />
        <Button variant="secondary" loading={rename.isPending} disabled={name === instance.name} onClick={() => rename.mutate()}>
          Rename
        </Button>
        <Input className="w-56 px-2 py-1.5" type="file" accept=".zip,application/zip" onChange={(e) => selectZip(e.target.files?.[0])} />
        <Button variant="secondary" loading={deploy.isPending} disabled={busy || !zipFile} onClick={() => deploy.mutate()}>
          <Upload className="h-4 w-4" />
          Deploy
        </Button>
        {instance.status === "running" ? (
          <Button variant="secondary" loading={stop.isPending} disabled={busy} onClick={() => stop.mutate()}>
            <Square className="h-4 w-4" />
            Stop
          </Button>
        ) : null}
        {instance.status === "stopped" ? (
          <Button variant="secondary" loading={start.isPending} disabled={busy} onClick={() => start.mutate()}>
            <Play className="h-4 w-4" />
            Start
          </Button>
        ) : null}
        <Button variant="danger" loading={del.isPending} disabled={busy} onClick={confirmDelete}>
          <Trash2 className="h-4 w-4" />
          Delete
        </Button>
      </div>
      {fileError ? <p className="text-sm text-rose-700">{fileError}</p> : null}
      <ApiError error={rename.error || start.error || stop.error || deploy.error || del.error} />
    </Panel>
  );
}

function ApiError({ error }: { error: unknown }) {
  if (!error) return null;
  const message = extractErrorMessage(error);
  return <p className="text-sm text-rose-700">{message}</p>;
}

function extractErrorMessage(error: unknown): string {
  if (typeof error !== "object" || error === null) {
    return "Request failed";
  }
  const response = "response" in error ? error.response : undefined;
  if (typeof response === "object" && response !== null) {
    const data = "data" in response ? response.data : undefined;
    const detail = firstDetailMessage(data);
    if (detail) return detail;
    if ("message" in response && typeof response.message === "string" && !response.message.startsWith("Something went wrong")) {
      return response.message;
    }
  }
  if (error instanceof Error && !error.message.startsWith("Something went wrong")) {
    return error.message;
  }
  return "Request failed. Refresh and try again.";
}

function firstDetailMessage(data: unknown): string {
  if (typeof data !== "object" || data === null) return "";
  for (const value of Object.values(data)) {
    if (typeof value === "object" && value !== null && "message" in value && typeof value.message === "string") {
      return value.message;
    }
  }
  return "";
}

function submit(event: FormEvent, fn: () => void) {
  event.preventDefault();
  fn();
}
