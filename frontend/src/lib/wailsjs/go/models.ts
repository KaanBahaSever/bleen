export namespace app {
	
	export class Automation {
	    supported: boolean;
	    enabled: boolean;
	    time: string;
	
	    static createFrom(source: any = {}) {
	        return new Automation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.enabled = source["enabled"];
	        this.time = source["time"];
	    }
	}
	export class BackupInfo {
	    id: string;
	    kind: string;
	    generation: number;
	    // Go type: time
	    finishedAt: any;
	    new: number;
	    modified: number;
	    deleted: number;
	    skipped: number;
	    stored: number;
	    sourceSize: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.generation = source["generation"];
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.new = source["new"];
	        this.modified = source["modified"];
	        this.deleted = source["deleted"];
	        this.skipped = source["skipped"];
	        this.stored = source["stored"];
	        this.sourceSize = source["sourceSize"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackupSource {
	    id: string;
	    name: string;
	    folder: string;
	    origin: string;
	    host: string;
	    backups: BackupInfo[];
	
	    static createFrom(source: any = {}) {
	        return new BackupSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.folder = source["folder"];
	        this.origin = source["origin"];
	        this.host = source["host"];
	        this.backups = this.convertValues(source["backups"], BackupInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BrowseEntry {
	    name: string;
	    path: string;
	    dir: boolean;
	    size: number;
	    // Go type: time
	    modTime: any;
	    changed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BrowseEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	        this.size = source["size"];
	        this.modTime = this.convertValues(source["modTime"], null);
	        this.changed = source["changed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BrowseResult {
	    entries: BrowseEntry[];
	    totalFiles: number;
	    totalBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new BrowseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], BrowseEntry);
	        this.totalFiles = source["totalFiles"];
	        this.totalBytes = source["totalBytes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobError {
	    code: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new JobError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class RunRecord {
	    id: string;
	    kind: string;
	    source: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    finishedAt: any;
	    result: string;
	    backupKind?: string;
	    files: number;
	    deduped: number;
	    bytes: number;
	    stored: number;
	    verified: boolean;
	    archives?: string[];
	    dest?: string;
	    issues?: archive.Issue[];
	    error?: JobError;
	    pruned?: number;
	
	    static createFrom(source: any = {}) {
	        return new RunRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.source = source["source"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.result = source["result"];
	        this.backupKind = source["backupKind"];
	        this.files = source["files"];
	        this.deduped = source["deduped"];
	        this.bytes = source["bytes"];
	        this.stored = source["stored"];
	        this.verified = source["verified"];
	        this.archives = source["archives"];
	        this.dest = source["dest"];
	        this.issues = this.convertValues(source["issues"], archive.Issue);
	        this.error = this.convertValues(source["error"], JobError);
	        this.pruned = source["pruned"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobState {
	    id: string;
	    kind: string;
	    phase: string;
	    sourceName: string;
	    index: number;
	    count: number;
	    scanned: number;
	    plan?: engine.Plan;
	    progress: engine.CopyProgress;
	    results: RunRecord[];
	    error?: JobError;
	    dest?: string;
	    paused: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JobState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.phase = source["phase"];
	        this.sourceName = source["sourceName"];
	        this.index = source["index"];
	        this.count = source["count"];
	        this.scanned = source["scanned"];
	        this.plan = this.convertValues(source["plan"], engine.Plan);
	        this.progress = this.convertValues(source["progress"], engine.CopyProgress);
	        this.results = this.convertValues(source["results"], RunRecord);
	        this.error = this.convertValues(source["error"], JobError);
	        this.dest = source["dest"];
	        this.paused = source["paused"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class KnownVault {
	    id: string;
	    label: string;
	    path: string;
	    connected: boolean;
	    active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnownVault(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.path = source["path"];
	        this.connected = source["connected"];
	        this.active = source["active"];
	    }
	}
	
	export class SourceState {
	    id: string;
	    name: string;
	    path: string;
	    reachable?: boolean;
	    vaultSourceId: string;
	    // Go type: time
	    lastBackup: any;
	    backups: number;
	    lastIssues: number;
	    recent: number[];
	
	    static createFrom(source: any = {}) {
	        return new SourceState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.reachable = source["reachable"];
	        this.vaultSourceId = source["vaultSourceId"];
	        this.lastBackup = this.convertValues(source["lastBackup"], null);
	        this.backups = source["backups"];
	        this.lastIssues = source["lastIssues"];
	        this.recent = source["recent"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VaultState {
	    id: string;
	    encrypted: boolean;
	    locked: boolean;
	    path: string;
	    label: string;
	    connected: boolean;
	    free: number;
	    fsType: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new VaultState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.encrypted = source["encrypted"];
	        this.locked = source["locked"];
	        this.path = source["path"];
	        this.label = source["label"];
	        this.connected = source["connected"];
	        this.free = source["free"];
	        this.fsType = source["fsType"];
	        this.error = source["error"];
	    }
	}
	export class State {
	    version: string;
	    language: string;
	    theme: string;
	    confirmBeforeRun: boolean;
	    exclude: string[];
	    defaultExclude: string[];
	    vault?: VaultState;
	    vaults: KnownVault[];
	    sources: SourceState[];
	    job?: JobState;
	    os: string;
	    keepGenerations: number;
	    newFullEvery: number;
	    automation: Automation;
	    notice: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.language = source["language"];
	        this.theme = source["theme"];
	        this.confirmBeforeRun = source["confirmBeforeRun"];
	        this.exclude = source["exclude"];
	        this.defaultExclude = source["defaultExclude"];
	        this.vault = this.convertValues(source["vault"], VaultState);
	        this.vaults = this.convertValues(source["vaults"], KnownVault);
	        this.sources = this.convertValues(source["sources"], SourceState);
	        this.job = this.convertValues(source["job"], JobState);
	        this.os = source["os"];
	        this.keepGenerations = source["keepGenerations"];
	        this.newFullEvery = source["newFullEvery"];
	        this.automation = this.convertValues(source["automation"], Automation);
	        this.notice = source["notice"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace archive {
	
	export class Issue {
	    path: string;
	    code: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Issue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}

}

export namespace engine {
	
	export class CopyProgress {
	    filesDone: number;
	    filesTotal: number;
	    bytesDone: number;
	    bytesTotal: number;
	    current: string;
	
	    static createFrom(source: any = {}) {
	        return new CopyProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filesDone = source["filesDone"];
	        this.filesTotal = source["filesTotal"];
	        this.bytesDone = source["bytesDone"];
	        this.bytesTotal = source["bytesTotal"];
	        this.current = source["current"];
	    }
	}
	export class Plan {
	    sourceName: string;
	    origin: string;
	    kind: string;
	    // Go type: time
	    lastFull: any;
	    // Go type: time
	    lastBackup: any;
	    totalFiles: number;
	    new: number;
	    modified: number;
	    deleted: number;
	    bytesToRead: number;
	    estStored: number;
	    vaultFree: number;
	    massChange: boolean;
	    scanIssues: number;
	    nothingToDo: boolean;
	    changedRatio: number;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceName = source["sourceName"];
	        this.origin = source["origin"];
	        this.kind = source["kind"];
	        this.lastFull = this.convertValues(source["lastFull"], null);
	        this.lastBackup = this.convertValues(source["lastBackup"], null);
	        this.totalFiles = source["totalFiles"];
	        this.new = source["new"];
	        this.modified = source["modified"];
	        this.deleted = source["deleted"];
	        this.bytesToRead = source["bytesToRead"];
	        this.estStored = source["estStored"];
	        this.vaultFree = source["vaultFree"];
	        this.massChange = source["massChange"];
	        this.scanIssues = source["scanIssues"];
	        this.nothingToDo = source["nothingToDo"];
	        this.changedRatio = source["changedRatio"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace platform {
	
	export class Volume {
	    path: string;
	    label: string;
	    fsType: string;
	    removable: boolean;
	    free: number;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.label = source["label"];
	        this.fsType = source["fsType"];
	        this.removable = source["removable"];
	        this.free = source["free"];
	        this.total = source["total"];
	    }
	}

}

