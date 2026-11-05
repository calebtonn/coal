export type Status = 'active' | 'paused' | 'done' | 'abandoned';
export type Source = 'user' | 'model';

export interface SearchResult {
  id: string;
  summary: string;
  source: Source;
  status: Status;
  lastActivityAt: string;
  updatedAt: string;
  artifactCount: number;
  nextStep: string | null;
  snippet: string | null;
  matched: string[];
}

export interface Provenance { tool: string; model?: string; sessionRef?: string; capturedBy?: string }
export interface ArtifactVersion { hash: string; size?: number; createdAt: string; producer?: Provenance }

export interface Artifact {
  hash: string;
  type: 'html' | 'code' | 'doc' | 'data' | 'image' | 'other';
  title?: string;
  mime?: string;
  uri?: string;
  size?: number;
  createdAt?: string;
  versions?: ArtifactVersion[];
  skipped?: string;
}

export interface Manifest {
  id: string;
  rev?: number;
  intent: { summary: string; goal?: string; tags?: string[]; source?: Source };
  status: Status;
  artifacts: Artifact[];
  state: { lastActivityAt: string; nextStep?: string; openQuestions?: string[]; summary?: string };
  provenance: Provenance;
  contexts?: { kind: string; key: string; label?: string }[];
  createdAt: string;
  updatedAt: string;
  [key: string]: unknown;
}
