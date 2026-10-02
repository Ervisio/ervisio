//#region \0rolldown/runtime.js
var e = Object.defineProperty, t = (t, n) => {
	let r = {};
	for (var i in t) e(r, i, {
		get: t[i],
		enumerable: !0
	});
	return n || e(r, Symbol.toStringTag, { value: "Module" }), r;
}, n;
function r(e) {
	n = e;
}
function i() {
	if (!n) throw Error("React is not ready: setReact() must run in activate() before rendering.");
	return n;
}
function a(e) {
	return ((...t) => i()[e](...t));
}
var o = Symbol.for("react.fragment"), s = a("createElement"), c = a("useState"), l = a("useEffect"), u = a("useRef"), d = a("useMemo"), f = a("useSyncExternalStore");
new Proxy({}, { get: (e, t) => i()[t] });
//#endregion
//#region src/sdk.ts
var p;
function m(e) {
	p = e;
}
function h() {
	if (!p) throw Error("The SDK is not ready: it is set in activate().");
	return p;
}
//#endregion
//#region src/i18n/areas/common.ts
var ee = /* @__PURE__ */ t({ default: () => g }), g = {
	en: {
		"common.back": "Back",
		"common.refresh": "Refresh",
		"common.retry": "Try again",
		"common.cancel": "Cancel",
		"common.close": "Close",
		"common.start": "Start",
		"common.stop": "Stop",
		"common.restart": "Restart",
		"common.pause": "Pause",
		"common.remove": "Remove",
		"common.open": "Open",
		"common.save": "Save",
		"common.edit": "Edit",
		"common.search": "Search",
		"common.loading": "Loading",
		"common.all": "All",
		"common.copy": "Copy",
		"common.never": "Never",
		"common.none": "None",
		"common.yes": "Yes",
		"common.no": "No",
		"common.openedBlocked": "This browser blocked the new tab. Open {url} yourself.",
		"state.running": "Running",
		"state.restarting": "Restarting",
		"state.paused": "Paused",
		"state.exited": "Stopped",
		"state.created": "Created",
		"state.dead": "Dead",
		"state.removing": "Removing",
		"health.healthy": "Healthy",
		"health.unhealthy": "Unhealthy",
		"health.starting": "Starting",
		"disk.title": "Disk used by Docker",
		"disk.total": "{size} in total",
		"disk.reclaimable": "{size} can be freed",
		"disk.images": "Images",
		"disk.containers": "Containers",
		"disk.volumes": "Volumes",
		"disk.cache": "Build cache",
		"disk.loading": "Measuring disk use. This can take a few seconds.",
		"disk.cleanup": "Clean up",
		"error.unreachable.title": "Docker is not reachable",
		"error.unreachable.text": "Docker is not installed or not running, or its socket is missing. Start it with \"systemctl start docker\", then try again.",
		"error.forbidden.title": "No permission to use Docker",
		"error.forbidden.text": "Your account may not talk to Docker. Add it to the docker group, or use an administrator account.",
		"error.admin.title": "Administrator rights needed",
		"error.admin.text": "Docker needs administrator rights on this machine. Unlock them when asked, then try again.",
		"error.engine.title": "Docker refused the request",
		"error.unknown.title": "Something went wrong",
		"error.detail": "Details: {message}",
		"soon.title": "{name} is not built yet",
		"soon.text": "This part of the Docker plugin is still being written."
	},
	it: {
		"common.back": "Indietro",
		"common.refresh": "Aggiorna",
		"common.retry": "Riprova",
		"common.cancel": "Annulla",
		"common.close": "Chiudi",
		"common.start": "Avvia",
		"common.stop": "Ferma",
		"common.restart": "Riavvia",
		"common.pause": "Metti in pausa",
		"common.remove": "Rimuovi",
		"common.open": "Apri",
		"common.save": "Salva",
		"common.edit": "Modifica",
		"common.search": "Cerca",
		"common.loading": "Caricamento",
		"common.all": "Tutti",
		"common.copy": "Copia",
		"common.never": "Mai",
		"common.none": "Nessuno",
		"common.yes": "Sì",
		"common.no": "No",
		"common.openedBlocked": "Il browser ha bloccato la nuova scheda. Apri {url} da solo.",
		"state.running": "In esecuzione",
		"state.restarting": "Riavvio in corso",
		"state.paused": "In pausa",
		"state.exited": "Fermo",
		"state.created": "Creato",
		"state.dead": "Morto",
		"state.removing": "Rimozione",
		"health.healthy": "In salute",
		"health.unhealthy": "Non in salute",
		"health.starting": "In avvio",
		"disk.title": "Spazio usato da Docker",
		"disk.total": "{size} in totale",
		"disk.reclaimable": "{size} liberabili",
		"disk.images": "Immagini",
		"disk.containers": "Container",
		"disk.volumes": "Volumi",
		"disk.cache": "Cache di build",
		"disk.loading": "Calcolo dello spazio in corso. Può richiedere qualche secondo.",
		"disk.cleanup": "Pulisci",
		"error.unreachable.title": "Docker non è raggiungibile",
		"error.unreachable.text": "Docker non è installato o non è in esecuzione, oppure manca il suo socket. Avvialo con \"systemctl start docker\" e riprova.",
		"error.forbidden.title": "Nessun permesso per usare Docker",
		"error.forbidden.text": "Il tuo account non può parlare con Docker. Aggiungilo al gruppo docker oppure usa un account amministratore.",
		"error.admin.title": "Servono i diritti di amministratore",
		"error.admin.text": "Docker richiede i diritti di amministratore su questa macchina. Sbloccali quando richiesto e riprova.",
		"error.engine.title": "Docker ha rifiutato la richiesta",
		"error.unknown.title": "Qualcosa è andato storto",
		"error.detail": "Dettagli: {message}",
		"soon.title": "{name} non è ancora pronto",
		"soon.text": "Questa parte del plugin Docker è ancora in lavorazione."
	}
}, te = /* @__PURE__ */ t({ default: () => ne }), ne = {
	en: {
		"containers.title": "Containers",
		"containers.new": "New container",
		"containers.standalone": "Standalone",
		"containers.stackRunning": "{running} of {total} running",
		"containers.filter.all": "All",
		"containers.filter.running": "Running",
		"containers.filter.stopped": "Stopped",
		"containers.filter.unhealthy": "Unhealthy",
		"containers.sub.none": "No containers yet",
		"containers.sub.running": "{n} running",
		"containers.sub.restarting": "{n} restarting",
		"containers.sub.stopped": "{n} stopped",
		"containers.sub.paused": "{n} paused",
		"containers.empty.title": "No containers yet",
		"containers.empty.text": "Create one from an image or a template. It shows up here with live CPU and memory.",
		"containers.empty.template": "Browse templates",
		"containers.noMatch.title": "No container matches",
		"containers.noMatch.text": "Change the filter or clear the search box.",
		"containers.cpu": "CPU",
		"containers.memory": "Memory",
		"containers.uptime": "Status",
		"containers.openStack": "Open stack",
		"containers.restartStack": "Restart stack",
		"containers.selectAll": "Select all",
		"containers.select": "Select {name}",
		"containers.port": "Open port {port}",
		"containers.openContainer": "Open {name}",
		"containers.selected": "{n} selected",
		"containers.clearSelection": "Clear selection",
		"containers.removeTitle": "Remove {name}?",
		"containers.removeMany": "Remove {n} containers?",
		"containers.removeDesc": "Running containers are stopped first. Their writable layer is deleted. Volumes stay unless you tick the box below.",
		"containers.stale": "Docker stopped answering. Showing the last known list.",
		"containers.removeVolumes": "Also remove anonymous volumes",
		"containers.done.start": "{name} started",
		"containers.done.stop": "{name} stopped",
		"containers.done.restart": "{name} restarted",
		"containers.done.remove": "{name} removed",
		"containers.fail.start": "Could not start {name}",
		"containers.fail.stop": "Could not stop {name}",
		"containers.fail.restart": "Could not restart {name}",
		"containers.fail.remove": "Could not remove {name}",
		"containers.bulkDone.start": "{n} containers started",
		"containers.bulkDone.stop": "{n} containers stopped",
		"containers.bulkDone.restart": "{n} containers restarted",
		"containers.bulkDone.remove": "{n} containers removed",
		"containers.bulkFail": "{failed} of {total} failed",
		"containers.widget.running": "running",
		"containers.widget.total": "{n} in total",
		"containers.widget.problems": "{n} need attention",
		"containers.widget.allWell": "All containers are fine",
		"containers.widget.open": "Open Docker"
	},
	it: {
		"containers.title": "Container",
		"containers.new": "Nuovo container",
		"containers.standalone": "Singoli",
		"containers.stackRunning": "{running} di {total} in esecuzione",
		"containers.filter.all": "Tutti",
		"containers.filter.running": "In esecuzione",
		"containers.filter.stopped": "Fermi",
		"containers.filter.unhealthy": "Con problemi",
		"containers.sub.none": "Ancora nessun container",
		"containers.sub.running": "{n} in esecuzione",
		"containers.sub.restarting": "{n} in riavvio",
		"containers.sub.stopped": "{n} fermi",
		"containers.sub.paused": "{n} in pausa",
		"containers.empty.title": "Ancora nessun container",
		"containers.empty.text": "Creane uno da un’immagine o da un modello. Comparirà qui con CPU e memoria in tempo reale.",
		"containers.empty.template": "Sfoglia i modelli",
		"containers.noMatch.title": "Nessun container corrisponde",
		"containers.noMatch.text": "Cambia il filtro o svuota la ricerca.",
		"containers.cpu": "CPU",
		"containers.memory": "Memoria",
		"containers.uptime": "Stato",
		"containers.openStack": "Apri lo stack",
		"containers.restartStack": "Riavvia lo stack",
		"containers.selectAll": "Seleziona tutti",
		"containers.select": "Seleziona {name}",
		"containers.port": "Apri la porta {port}",
		"containers.openContainer": "Apri {name}",
		"containers.selected": "{n} selezionati",
		"containers.clearSelection": "Annulla la selezione",
		"containers.removeTitle": "Rimuovere {name}?",
		"containers.removeMany": "Rimuovere {n} container?",
		"containers.removeDesc": "I container in esecuzione vengono prima fermati. Il loro livello scrivibile viene eliminato. I volumi restano, a meno che tu non spunti la casella qui sotto.",
		"containers.stale": "Docker ha smesso di rispondere. Mostro l’ultimo elenco noto.",
		"containers.removeVolumes": "Rimuovi anche i volumi anonimi",
		"containers.done.start": "{name} avviato",
		"containers.done.stop": "{name} fermato",
		"containers.done.restart": "{name} riavviato",
		"containers.done.remove": "{name} rimosso",
		"containers.fail.start": "Impossibile avviare {name}",
		"containers.fail.stop": "Impossibile fermare {name}",
		"containers.fail.restart": "Impossibile riavviare {name}",
		"containers.fail.remove": "Impossibile rimuovere {name}",
		"containers.bulkDone.start": "{n} container avviati",
		"containers.bulkDone.stop": "{n} container fermati",
		"containers.bulkDone.restart": "{n} container riavviati",
		"containers.bulkDone.remove": "{n} container rimossi",
		"containers.bulkFail": "{failed} su {total} non riusciti",
		"containers.widget.running": "attivi",
		"containers.widget.total": "{n} in totale",
		"containers.widget.problems": "{n} richiedono attenzione",
		"containers.widget.allWell": "Tutti i container sono a posto",
		"containers.widget.open": "Apri Docker"
	}
}, _ = /* @__PURE__ */ t({ default: () => v }), v = {
	en: {
		"nav.workloads": "Workloads",
		"nav.resources": "Resources",
		"nav.tools": "Tools",
		"nav.containers": "Containers",
		"nav.stacks": "Stacks",
		"nav.templates": "Templates",
		"nav.images": "Images",
		"nav.volumes": "Volumes",
		"nav.networks": "Networks",
		"nav.registries": "Registries",
		"nav.cleanup": "Cleanup",
		"nav.autoupdate": "Auto-update",
		"nav.alerts": "Alerts",
		"nav.aria": "Docker sections",
		"shell.search": "Search containers, images, stacks",
		"shell.engine": "Engine {version}",
		"shell.oldSdk.title": "This LinuxAdmin is too old for the Docker plugin",
		"shell.oldSdk.text": "Docker 2 needs plugin SDK version 3 or newer. Update LinuxAdmin, then reload."
	},
	it: {
		"nav.workloads": "Carichi",
		"nav.resources": "Risorse",
		"nav.tools": "Strumenti",
		"nav.containers": "Container",
		"nav.stacks": "Stack",
		"nav.templates": "Modelli",
		"nav.images": "Immagini",
		"nav.volumes": "Volumi",
		"nav.networks": "Reti",
		"nav.registries": "Registri",
		"nav.cleanup": "Pulizia",
		"nav.autoupdate": "Auto-aggiornamento",
		"nav.alerts": "Avvisi",
		"nav.aria": "Sezioni di Docker",
		"shell.search": "Cerca container, immagini, stack",
		"shell.engine": "Engine {version}",
		"shell.oldSdk.title": "Questo LinuxAdmin è troppo vecchio per il plugin Docker",
		"shell.oldSdk.text": "Docker 2 richiede la versione 3 o successiva dell’SDK dei plugin. Aggiorna LinuxAdmin e ricarica."
	}
}, y = /* #__PURE__ */ Object.assign({
	"./areas/common.ts": ee,
	"./areas/containers.ts": te,
	"./areas/shell.ts": _
});
function re() {
	let e = {
		en: {},
		it: {}
	};
	for (let t of Object.keys(y).sort()) {
		let n = y[t].default;
		Object.assign(e.en, n.en), Object.assign(e.it, n.it);
	}
	return e;
}
function ie() {
	let e = re();
	h().registerStrings({
		en: e.en,
		it: e.it
	});
}
var b = (e, t) => h().t(e, t), x = ".dk-root{font-variant-numeric:tabular-nums;color:var(--ink);box-sizing:border-box;min-height:100%;padding:20px 22px 28px}@media (max-width:900px){.dk-root{padding:12px 12px 24px}}.dk-root *,.dk-root :before,.dk-root :after{box-sizing:border-box}.dk-muted{color:var(--ink3);font-size:12.5px;font-weight:600}.dk-mono{font-family:var(--mono);font-size:12.5px}.dk-num{text-align:right;white-space:nowrap;font-family:var(--mono);font-size:12.5px}.dk-ph{flex-wrap:wrap;align-items:center;gap:14px;margin-bottom:18px;display:flex}.dk-ph-ic{border-radius:var(--r-icon);background:var(--s,var(--acc-s));width:46px;height:46px;color:var(--h,var(--acc));flex:none;place-items:center;display:grid}.dk-ph-ic .ui-icon{width:22px;height:22px}.dk-ph-tx{min-width:0}.dk-ph-tx h1{letter-spacing:-.02em;margin:0;font-size:26px;font-weight:800;line-height:1.15}.dk-ph-tx p{color:var(--ink2);margin:2px 0 0;font-size:13.5px}.dk-ph-act{flex-wrap:wrap;align-items:center;gap:8px;margin-left:auto;display:flex}.dk-card{background:var(--sunk);border-radius:var(--r-card);flex-direction:column;gap:12px;padding:16px 18px;display:flex}.dk-card-h{flex-wrap:wrap;align-items:center;gap:12px;display:flex}.dk-card-h h3{margin:0 auto 0 0;font-size:15px;font-weight:800}.dk-dot{background:var(--ink3);border-radius:50%;flex:none;width:10px;height:10px}.dk-dot--ok{background:var(--ok)}.dk-dot--warn{background:var(--warn)}.dk-dot--info{background:var(--info)}.dk-dot--err{background:var(--err)}.dk-meter{background:color-mix(in srgb, var(--ink3) 22%, transparent);border-radius:3px;height:6px;display:block;overflow:hidden}.dk-meter i{background:var(--ok);border-radius:3px;height:100%;transition:width .4s;display:block}.dk-meter--warn i{background:var(--warn)}.dk-meter--err i{background:var(--err)}.dk-spark{width:100%;height:28px}.dk-spark .ui-spark{width:100%}@media (prefers-reduced-motion:reduce){.dk-meter i{transition:none}}.dk-sbar{border-radius:7px;gap:3px;height:14px;display:flex;overflow:hidden}.dk-sbar-part{gap:0;min-width:4px;display:flex}.dk-sbar-part i{background:var(--h);height:100%;display:block}.dk-sbar-part i.dk-sbar-free{opacity:.35}.dk-legend{flex-wrap:wrap;gap:18px;display:flex}.dk-legend>div{flex-wrap:wrap;align-items:center;gap:8px;font-size:13px;display:flex}.dk-sw{background:var(--h);border-radius:4px;width:12px;height:12px}.dk-tablewrap{overflow-x:auto}.dk-table{border-collapse:separate;border-spacing:0 3px;width:100%;font-size:13.5px}.dk-table th{text-align:left;color:var(--ink3);white-space:nowrap;padding:8px 10px;font-size:12.5px;font-weight:700}.dk-table td{background:var(--sunk);padding:9px 10px}.dk-table tr td:first-child{border-radius:12px 0 0 12px}.dk-table tr td:last-child{border-radius:0 12px 12px 0}.dk-table .dk-num{text-align:right}.dk-table .dk-cb{width:36px}.dk-table tr:hover td{background:color-mix(in srgb, var(--sunk) 75%, var(--ink3))}.dk-table tr.dk-click{cursor:pointer}.dk-table tr.dk-sel td{background:var(--acc-s)}.dk-table tr.dk-bad td:first-child{box-shadow:inset 3px 0 0 var(--err)}.dk-tag{font:600 12px var(--mono);background:var(--surface);color:var(--ink2);border-radius:7px;padding:2px 7px;display:inline-flex}.dk-used{flex-wrap:wrap;gap:4px;display:flex}.dk-used span{background:var(--acc-s);color:var(--acc);border-radius:8px;padding:2px 8px;font-size:12px;font-weight:700}.dk-used span.dk-unused{background:var(--warn-s);color:var(--warn)}@media (max-width:1100px){.dk-hide-md{display:none}}@media (max-width:760px){.dk-hide-sm{display:none}}.dk-port{font:600 12px var(--mono);background:var(--acc-s);color:var(--acc);cursor:pointer;border-radius:7px;align-items:center;gap:4px;padding:2px 7px;display:inline-flex}.dk-port:hover{background:color-mix(in srgb, var(--acc) 22%, var(--sunk))}.dk-port .ui-icon{width:11px;height:11px}", ae = ".dk-chips{flex-wrap:wrap;gap:6px;margin-bottom:16px;display:flex}.dk-stale{margin:0 0 12px}.dk-groups{flex-direction:column;gap:22px;display:flex}.dk-stack{flex-direction:column;gap:10px;display:flex}.dk-stack-h{flex-wrap:wrap;align-items:center;gap:12px;display:flex}.dk-stack-n{align-items:center;gap:8px;font-size:16px;font-weight:800;display:inline-flex}.dk-stack-n .ui-icon{width:18px;height:18px;color:var(--acc)}.dk-stack-act{flex-wrap:wrap;align-items:center;gap:6px;margin-left:auto;display:flex}.dk-grid{grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:10px;display:grid}.dk-cc{background:var(--sunk);border-radius:var(--r-card);cursor:pointer;outline:none;flex-direction:column;gap:10px;min-width:0;padding:14px;transition:background .15s;display:flex;position:relative}.dk-cc:hover{background:color-mix(in srgb, var(--sunk) 75%, var(--ink3))}.dk-cc:focus-visible{box-shadow:0 0 0 2px var(--acc)}.dk-cc--bad{box-shadow:inset 0 0 0 2px var(--err)}.dk-cc--bad:focus-visible{box-shadow:inset 0 0 0 2px var(--err), 0 0 0 2px var(--acc)}.dk-cc--sel{background:var(--acc-s)}.dk-cc-t{align-items:center;gap:10px;min-width:0;display:flex}.dk-cc-tx{flex:1;min-width:0}.dk-cc-tx b{text-overflow:ellipsis;white-space:nowrap;font-weight:800;display:block;overflow:hidden}.dk-cc-img{font:500 12px var(--mono);color:var(--ink3);white-space:nowrap;text-overflow:ellipsis;display:block;overflow:hidden}.dk-cc-ck{flex:none;place-items:center;display:grid}.dk-cc-m{grid-template-columns:1fr 1fr;gap:10px;display:grid}.dk-cc-m small{color:var(--ink3);justify-content:space-between;gap:6px;margin-bottom:4px;font-size:11.5px;display:flex}.dk-cc-m small span{color:var(--ink);font:600 12px var(--mono)}.dk-cc-st{color:var(--ink3);white-space:nowrap;text-overflow:ellipsis;font-size:12px;overflow:hidden}.dk-cc-f{align-items:center;gap:6px;min-height:30px;display:flex}.dk-cc-ports{flex-wrap:wrap;flex:1;gap:4px;min-width:0;display:flex}.dk-cc-act{gap:2px;margin-left:auto;display:flex}.dk-cc-act .ui-btn{color:var(--ink2)}.dk-bulk{border-radius:var(--r-btn);background:var(--ink);color:var(--surface);z-index:5;flex-wrap:wrap;align-self:center;align-items:center;gap:2px;max-width:100%;margin-top:18px;padding:6px 6px 6px 18px;display:flex;position:sticky;bottom:12px;box-shadow:0 14px 30px -10px #0009}.dk-bulk>b{margin-right:10px;font-weight:800}.dk-bulk button{font:inherit;color:var(--surface);cursor:pointer;background:0 0;border:0;border-radius:10px;align-items:center;gap:6px;padding:8px 12px;font-size:13px;font-weight:700;display:inline-flex}.dk-bulk button:hover{background:color-mix(in srgb, var(--surface) 14%, transparent)}.dk-bulk button:focus-visible{outline:2px solid var(--surface)}.dk-bulk .ui-icon{width:16px;height:16px}.dk-rm-list{background:var(--sunk);font:500 12.5px var(--mono);border-radius:12px;max-height:120px;margin:8px 0 12px;padding:10px 12px;overflow:auto}.dk-w-open{margin-top:10px}", S = ".dk-shell{align-items:flex-start;gap:14px;display:flex}.dk-nav{background:var(--surface);border-radius:var(--r-card);flex-direction:column;flex:none;gap:2px;width:220px;padding:12px;display:flex;position:sticky;top:0}.dk-nav-gl{color:var(--ink3);margin:12px 10px 4px;font-size:12px;font-weight:700}.dk-nav-gl:first-child{margin-top:2px}.dk-nav-it{border-radius:var(--r-btn);color:var(--ink2);font:inherit;cursor:pointer;text-align:left;background:0 0;border:0;align-items:center;gap:10px;width:100%;padding:9px 10px;font-weight:700;display:flex}.dk-nav-it .ui-icon{flex:none;width:17px;height:17px}.dk-nav-it b{color:var(--ink3);margin-left:auto;font-size:12px;font-weight:700}.dk-nav-it:hover{background:var(--sunk);color:var(--ink)}.dk-nav-it[aria-current=page]{background:var(--acc-s);color:var(--acc)}.dk-nav-it[aria-current=page] b{color:inherit}.dk-nav-ft{color:var(--ink3);align-items:center;gap:8px;margin:14px 10px 4px;font-size:12px;font-weight:600;display:flex}.dk-main{flex-direction:column;flex:1;gap:14px;min-width:0;display:flex}.dk-top{align-items:center;gap:10px;display:flex}.dk-search{max-width:520px}.dk-view{flex-direction:column;min-width:0;display:flex}@media (max-width:900px){.dk-shell{flex-direction:column;gap:10px;padding:0}.dk-nav{scrollbar-width:none;flex-direction:row;gap:6px;width:100%;padding:8px;position:static;overflow-x:auto}.dk-nav::-webkit-scrollbar{display:none}.dk-nav-gl,.dk-nav-ft{display:none}.dk-nav-it{white-space:nowrap;background:var(--sunk);border-radius:10px;flex:none;width:auto;padding:7px 12px}.dk-nav-it b{font-size:11.5px}.dk-main{width:100%}.dk-search{max-width:none}}", oe = ".xterm{cursor:text;-webkit-user-select:none;user-select:none;position:relative}.xterm.focus,.xterm:focus{outline:none}.xterm .xterm-helpers{z-index:5;position:absolute;top:0}.xterm .xterm-helper-textarea{opacity:0;z-index:-5;white-space:nowrap;resize:none;border:0;width:0;height:0;margin:0;padding:0;position:absolute;top:0;left:-9999em;overflow:hidden}.xterm .composition-view{color:#fff;white-space:nowrap;z-index:1;background:#000;display:none;position:absolute}.xterm .composition-view.active{display:block}.xterm .xterm-viewport{cursor:default;background-color:#000;position:absolute;inset:0;overflow-y:scroll}.xterm .xterm-screen{position:relative}.xterm .xterm-screen canvas{position:absolute;top:0;left:0}.xterm-char-measure-element{visibility:hidden;line-height:normal;display:inline-block;position:absolute;top:0;left:-9999em}.xterm.enable-mouse-events{cursor:default}.xterm.xterm-cursor-pointer,.xterm .xterm-cursor-pointer{cursor:pointer}.xterm.column-select.focus{cursor:crosshair}.xterm .xterm-accessibility:not(.debug),.xterm .xterm-message{z-index:10;color:#0000;pointer-events:none;position:absolute;inset:0}.xterm .xterm-accessibility-tree:not(.debug) ::selection{color:#0000}.xterm .xterm-accessibility-tree{-webkit-user-select:text;user-select:text;white-space:pre;font-family:monospace}.xterm .xterm-accessibility-tree>div{transform-origin:0;width:-moz-fit-content;width:fit-content}.xterm .live-region{width:1px;height:1px;position:absolute;left:-9999px;overflow:hidden}.xterm-dim{opacity:1!important}.xterm-underline-1{text-decoration:underline}.xterm-underline-2{-webkit-text-decoration:underline double;text-decoration:underline double}.xterm-underline-3{-webkit-text-decoration:underline wavy;text-decoration:underline wavy}.xterm-underline-4{-webkit-text-decoration:underline dotted;text-decoration:underline dotted}.xterm-underline-5{-webkit-text-decoration:underline dashed;text-decoration:underline dashed}.xterm-overline{text-decoration:overline}.xterm-overline.xterm-underline-1{text-decoration:underline overline}.xterm-overline.xterm-underline-2{-webkit-text-decoration:overline double underline;text-decoration:overline double underline}.xterm-overline.xterm-underline-3{-webkit-text-decoration:overline wavy underline;text-decoration:overline wavy underline}.xterm-overline.xterm-underline-4{-webkit-text-decoration:overline dotted underline;text-decoration:overline dotted underline}.xterm-overline.xterm-underline-5{-webkit-text-decoration:overline dashed underline;text-decoration:overline dashed underline}.xterm-strikethrough{text-decoration:line-through}.xterm-screen .xterm-decoration-container .xterm-decoration{z-index:6;position:absolute}.xterm-screen .xterm-decoration-container .xterm-decoration.xterm-decoration-top-layer{z-index:7}.xterm-decoration-overview-ruler{z-index:8;pointer-events:none;position:absolute;top:0;right:0}.xterm-decoration-top{z-index:2;position:relative}.xterm .xterm-scrollable-element>.scrollbar{cursor:default}.xterm .xterm-scrollable-element>.scrollbar>.scra{cursor:pointer;font-size:11px!important}.xterm .xterm-scrollable-element>.visible{opacity:1;z-index:11;background:0 0;transition:opacity .1s linear}.xterm .xterm-scrollable-element>.invisible{opacity:0;pointer-events:none}.xterm .xterm-scrollable-element>.invisible.fade{transition:opacity .8s linear}.xterm .xterm-scrollable-element>.shadow{display:none;position:absolute}.xterm .xterm-scrollable-element>.shadow.top{width:100%;height:3px;box-shadow:var(--vscode-scrollbar-shadow,#000) 0 6px 6px -6px inset;display:block;top:0;left:3px}.xterm .xterm-scrollable-element>.shadow.left{width:3px;height:100%;box-shadow:var(--vscode-scrollbar-shadow,#000) 6px 0 6px -6px inset;display:block;top:3px;left:0}.xterm .xterm-scrollable-element>.shadow.top-left-corner{width:3px;height:3px;display:block;top:0;left:0}.xterm .xterm-scrollable-element>.shadow.top.left{box-shadow:var(--vscode-scrollbar-shadow,#000) 6px 0 6px -6px inset}", se = /* #__PURE__ */ Object.assign({
	"./base.css": x,
	"./containers.css": ae,
	"./shell.css": S
});
function ce() {
	if (document.getElementById("dk-styles")) return;
	let e = document.createElement("style");
	e.id = "dk-styles", e.textContent = Object.keys(se).sort().map((e) => se[e]).join("\n") + "\n" + oe, document.head.appendChild(e);
}
//#endregion
//#region src/ui/icons.ts
function C() {
	let e = h().ui.registerIcon;
	e && (e("box", "<path d=\"M21 8l-9-5-9 5v8l9 5 9-5z\"/><path d=\"M3 8l9 5 9-5M12 13v8\"/>"), e("layers", "<path d=\"M12 3l8 4.5-8 4.5-8-4.5z\"/><path d=\"M4 12l8 4.5 8-4.5M4 16.5L12 21l8-4.5\"/>"), e("store", "<path d=\"M4 9l1.5-5h13L20 9\"/><path d=\"M4 9v10a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1V9\"/><path d=\"M4 9h16M9.5 13h5\"/>"), e("database", "<ellipse cx=\"12\" cy=\"5.5\" rx=\"7.5\" ry=\"2.8\"/><path d=\"M4.5 5.5v13c0 1.5 3.4 2.8 7.5 2.8s7.5-1.3 7.5-2.8v-13M4.5 12c0 1.5 3.4 2.8 7.5 2.8s7.5-1.3 7.5-2.8\"/>"));
}
var le = {
	containers: "box",
	stacks: "layers",
	templates: "store",
	images: "image",
	volumes: "database",
	networks: "net",
	registries: "key",
	cleanup: "broom",
	autoupdate: "refresh",
	alerts: "bell"
}, w = "docker", ue = class extends Error {
	status;
	code;
	constructor(e, t) {
		super(t), this.name = "DockerError", this.status = e, this.code = e === 404 ? "not_found" : e === 409 ? "conflict" : "engine";
	}
}, T;
function de() {
	return T ||= h().api.http(w, {
		method: "GET",
		path: "/version"
	}).then((e) => {
		if (e.status >= 400) throw me(e);
		return e.json();
	}).catch((e) => {
		throw T = void 0, e;
	}), T;
}
function fe() {
	T = void 0;
}
async function pe(e) {
	return `/v${(await de()).ApiVersion}${e}`;
}
function me(e) {
	let t = "";
	try {
		t = e.json().message ?? "";
	} catch {
		t = e.body?.slice(0, 300) ?? "";
	}
	return new ue(e.status, t || `Docker answered ${e.status}`);
}
async function he(e, t, n = {}) {
	return h().api.http(w, {
		method: e,
		path: await pe(t),
		...n
	});
}
async function E(e, t, n = {}) {
	let r = await he(e, t, n);
	if (r.status >= 400) throw me(r);
	if (r.body) try {
		return r.json();
	} catch {
		return r.body;
	}
}
function ge(e, t, n, r) {
	let i = !1, a;
	return pe(t).then((t) => {
		if (i) return;
		let o = !1, s = "";
		a = h().api.httpStream(w, {
			method: e,
			path: t,
			...n
		}, {
			onStart: (e, t) => {
				e >= 400 ? o = !0 : r.onStart?.(e, t);
			},
			onData: (e) => {
				o ? s += new TextDecoder().decode(e) : r.onData(e);
			},
			onEnd: () => {
				if (o) {
					let e = s;
					try {
						e = JSON.parse(s).message ?? s;
					} catch {}
					r.onError?.(new ue(500, e || "Docker refused the request"));
				} else r.onEnd?.();
			},
			onError: (e) => r.onError?.(e)
		});
	}).catch((e) => r.onError?.(e)), { close() {
		i = !0, a?.close();
	} };
}
var D = {
	get: (e, t) => E("GET", e, { query: t }),
	post: (e, t, n) => E("POST", e, {
		query: t,
		body: n
	}),
	put: (e, t, n) => E("PUT", e, {
		query: t,
		body: n
	}),
	delete: (e, t) => E("DELETE", e, { query: t }),
	request: he,
	json: E,
	stream: ge
};
function _e(e) {
	let t = e, n = t?.message ?? String(e);
	if (e instanceof ue) return {
		kind: "engine",
		message: n
	};
	switch (t?.code) {
		case "needs_admin": return {
			kind: "admin",
			message: n
		};
		case "forbidden": return {
			kind: "forbidden",
			message: n
		};
		case "unavailable":
		case "not_found": return {
			kind: "unreachable",
			message: n
		};
		default: return {
			kind: /ECONNREFUSED|no such file|connect/i.test(n) ? "unreachable" : "unknown",
			message: n
		};
	}
}
//#endregion
//#region src/api/streams.ts
var ve = class {
	onValue;
	onBad;
	buf = "";
	dec = new TextDecoder();
	constructor(e, t) {
		this.onValue = e, this.onBad = t;
	}
	push(e) {
		this.buf += this.dec.decode(e, { stream: !0 });
		let t;
		for (; (t = this.buf.indexOf("\n")) >= 0;) {
			let e = this.buf.slice(0, t).trim();
			if (this.buf = this.buf.slice(t + 1), e) try {
				this.onValue(JSON.parse(e));
			} catch {
				this.onBad?.(e);
			}
		}
	}
	end() {
		let e = (this.buf + this.dec.decode()).trim();
		if (this.buf = "", e) try {
			this.onValue(JSON.parse(e));
		} catch {
			this.onBad?.(e);
		}
	}
}, O = /* @__PURE__ */ new Set(), k, ye, A = 0, j = 0;
function be() {
	if (k || !O.size) return;
	let e = new ve((e) => {
		A = 0, j = e.time || j, O.forEach((t) => t(e));
	}), t = () => {
		if (!k || (k = void 0, !O.size)) return;
		A++, ye = setTimeout(be, Math.min(15e3, 1e3 * 2 ** Math.min(A, 4)));
		let e = {
			Type: "system",
			Action: "reconnect",
			time: Math.floor(Date.now() / 1e3)
		};
		O.forEach((t) => t(e));
	};
	k = D.stream("GET", "/events", j ? { query: { since: String(j + 1) } } : {}, {
		onData: (t) => e.push(t),
		onEnd: t,
		onError: t
	});
}
function xe(e) {
	return O.add(e), be(), () => {
		O.delete(e), O.size || (clearTimeout(ye), k?.close(), k = void 0, A = 0);
	};
}
//#endregion
//#region src/api/store.ts
function M(e, t) {
	let n = {
		data: void 0,
		error: null,
		loading: !0,
		updatedAt: 0
	}, r = /* @__PURE__ */ new Set(), i, a, o, s, c = (e) => {
		n = e, r.forEach((e) => e());
	}, l = () => {
		if (s) return s;
		let t = new Promise((e, t) => setTimeout(() => t(/* @__PURE__ */ Error("Docker did not answer in time")), 2e4));
		return s = Promise.race([e(), t]).then((e) => c({
			data: e,
			error: null,
			loading: !1,
			updatedAt: Date.now()
		})).catch((e) => c({
			data: n.data,
			error: _e(e),
			loading: !1,
			updatedAt: n.updatedAt
		})).finally(() => {
			s = void 0;
		}), s;
	}, u = () => {
		if (l(), i = setInterval(() => {
			document.hidden || l();
		}, t.intervalMs), t.refreshOn) {
			let e = t.refreshOn;
			a = xe((t) => {
				e(t) && (clearTimeout(o), o = setTimeout(() => void l(), 350));
			});
		}
	}, d = () => {
		clearInterval(i), clearTimeout(o), a?.(), i = void 0, a = void 0;
	}, p = (e) => (r.add(e), r.size === 1 && u(), () => {
		r.delete(e), r.size || d();
	});
	return {
		use: () => f(p, () => n),
		refresh: l,
		peek: () => n
	};
}
//#endregion
//#region src/api/resources.ts
var N = (...e) => (t) => e.includes(t.Type) && !t.Action.startsWith("exec_") && t.Action !== "attach" || t.Type === "system" && t.Action === "reconnect", P = M(() => D.get("/containers/json", { all: "1" }), {
	intervalMs: 5e3,
	refreshOn: N("container")
}), Se = M(() => D.get("/images/json", { "shared-size": "1" }), {
	intervalMs: 2e4,
	refreshOn: N("image", "container")
}), Ce = M(async () => (await D.get("/volumes")).Volumes ?? [], {
	intervalMs: 2e4,
	refreshOn: N("volume")
}), we = M(() => D.get("/networks"), {
	intervalMs: 2e4,
	refreshOn: N("network")
}), Te = M(async () => ({
	info: await D.get("/info"),
	version: await de()
}), { intervalMs: 3e4 }), Ee = [
	"B",
	"KB",
	"MB",
	"GB",
	"TB",
	"PB"
];
function De(e, t) {
	if (e == null || !isFinite(e) || e < 0) return "–";
	let n = e, r = 0;
	for (; n >= 1024 && r < Ee.length - 1;) n /= 1024, r++;
	let i = t ?? (r === 0 || n >= 100 ? 0 : n >= 10 ? 1 : 2);
	return `${n.toFixed(i).replace(/\.0+$/, "")} ${Ee[r]}`;
}
function Oe(e, t = 1) {
	return e == null || !isFinite(e) ? "–" : `${e.toFixed(e >= 100 ? 0 : t)}%`;
}
var F = (e) => (e.Names?.[0] ?? "").replace(/^\//, "") || "?", ke = (e) => e.replace(/^.*\//, "");
//#endregion
//#region src/api/model.ts
function Ae(e) {
	let t = /\((?:health: )?(healthy|unhealthy|starting)\)/.exec(e.Status);
	return t ? t[1] : "none";
}
var je = (e) => Ae(e) === "unhealthy" || e.State === "restarting", Me = (e) => [
	"exited",
	"created",
	"dead"
].includes(e.State), Ne = (e) => e.Labels?.["com.docker.compose.project"] ?? "";
function Pe(e) {
	let t = /* @__PURE__ */ new Map();
	for (let n of [...e].sort((e, t) => F(e).localeCompare(F(t)))) {
		let e = Ne(n);
		t.has(e) || t.set(e, []), t.get(e).push(n);
	}
	return [...t.entries()].sort(([e], [t]) => e === "" ? 1 : t === "" ? -1 : e.localeCompare(t)).map(([e, t]) => ({
		name: e,
		containers: t,
		running: t.filter((e) => e.State === "running").length
	}));
}
function Fe(e) {
	let t = /* @__PURE__ */ new Set(), n = [];
	for (let r of e.Ports ?? []) {
		if (!r.PublicPort) continue;
		let e = `${r.PublicPort}/${r.Type}`;
		t.has(e) || (t.add(e), n.push({
			host: r.PublicPort,
			container: r.PrivatePort,
			proto: r.Type
		}));
	}
	return n.sort((e, t) => e.host - t.host);
}
function Ie(e) {
	return {
		total: e.length,
		running: e.filter((e) => e.State === "running").length,
		restarting: e.filter((e) => e.State === "restarting").length,
		paused: e.filter((e) => e.State === "paused").length,
		stopped: e.filter(Me).length,
		problems: e.filter(je).length
	};
}
function Le(e, t) {
	let n = t.trim().toLowerCase();
	return !n || [
		F(e),
		e.Image,
		Ne(e),
		e.Id.slice(0, 12)
	].some((e) => e.toLowerCase().includes(n));
}
function Re(e) {
	let t = typeof location < "u" && location.hostname || "localhost";
	return `${e.container === 443 || e.container === 8443 ? "https" : "http"}://${t}:${e.host}`;
}
//#endregion
//#region src/kit.ts
function I(e) {
	let t = (t) => s(h().ui[e], t);
	return t.displayName = e, t;
}
var L = I("Button"), R = I("IconButton"), z = I("Icon"), ze = I("Input");
I("Textarea"), I("Select"), I("Field"), I("Switch");
var B = I("Checkbox");
I("Segmented"), I("Badge");
var Be = I("Chip");
I("Progress");
var Ve = I("Skeleton"), V = I("EmptyState");
I("Card");
var He = I("StatCard");
I("Tabs"), I("Dialog");
var Ue = I("ConfirmDialog");
I("Sheet"), I("Panel"), I("DropdownMenu"), I("Tooltip");
var We = I("Sparkline");
I("AreaChart");
var H = {
	ok: (e, t) => h().ui.toast.ok(e, t),
	err: (e, t) => h().ui.toast.err(e, t),
	info: (e, t) => h().ui.toast.info(e, t)
};
//#endregion
//#region src/router.ts
function Ge(e) {
	switch (e.view) {
		case "container":
		case "create": return "containers";
		case "stack": return "stacks";
		case "template": return "templates";
		default: return e.view;
	}
}
var Ke = { view: "containers" }, U = [Ke], W = /* @__PURE__ */ new Set(), qe = () => W.forEach((e) => e()), Je = (e, t) => JSON.stringify(e) === JSON.stringify(t);
function G(e, t = {}) {
	let n = U[U.length - 1];
	if (t.root) U = [e];
	else if (t.replace) U = [...U.slice(0, -1), e];
	else if (!Je(n, e)) U = [...U, e];
	else return;
	qe();
}
function Ye() {
	U = U.length > 1 ? U.slice(0, -1) : [Ke], qe();
}
var Xe = () => U[U.length - 1], Ze = () => U.length > 1, Qe = (e) => (W.add(e), () => W.delete(e));
function $e() {
	return f(Qe, Xe);
}
function et() {
	return f(Qe, Ze);
}
var tt = "", nt = /* @__PURE__ */ new Set();
function rt(e) {
	tt = e, nt.forEach((e) => e());
}
function it() {
	return f((e) => (nt.add(e), () => nt.delete(e)), () => tt);
}
//#endregion
//#region src/jsx-runtime-shim.ts
function K(e, t, n) {
	let { children: r, ...i } = t;
	return n !== void 0 && (i.key = n), r === void 0 ? s(e, i) : s(e, i, r);
}
function q(e, t, n) {
	let { children: r, ...i } = t;
	return n !== void 0 && (i.key = n), s(e, i, ...r);
}
//#endregion
//#region src/ui/ErrorState.tsx
function at({ error: e, onRetry: t }) {
	let n = e.kind === "unknown" ? "unknown" : e.kind, r = n === "engine" || n === "unknown" ? b("error.detail", { message: e.message }) : `${b(`error.${n}.text`)}`;
	return /* @__PURE__ */ K(V, {
		icon: "alert",
		hue: "svc",
		title: b(`error.${n}.title`),
		text: r,
		action: t && /* @__PURE__ */ K(L, {
			variant: "primary",
			icon: "refresh",
			onClick: t,
			children: b("common.retry")
		})
	});
}
//#endregion
//#region src/ui/PageHeader.tsx
function ot({ icon: e, hue: t = "file", title: n, subtitle: r, actions: i, back: a }) {
	let o = et();
	return /* @__PURE__ */ q("header", {
		className: `dk-ph hue-${t}`,
		children: [
			a && o && /* @__PURE__ */ K(R, {
				icon: "chevronleft",
				label: b("common.back"),
				onClick: Ye
			}),
			/* @__PURE__ */ K("span", {
				className: "dk-ph-ic",
				children: /* @__PURE__ */ K(z, { name: e })
			}),
			/* @__PURE__ */ q("div", {
				className: "dk-ph-tx",
				children: [/* @__PURE__ */ K("h1", { children: n }), r && /* @__PURE__ */ K("p", { children: r })]
			}),
			i && /* @__PURE__ */ K("div", {
				className: "dk-ph-act",
				children: i
			})
		]
	});
}
//#endregion
//#region src/ui/ComingSoon.tsx
function J({ icon: e, name: t, back: n }) {
	return /* @__PURE__ */ q(o, { children: [/* @__PURE__ */ K(ot, {
		icon: e,
		title: t,
		back: n
	}), /* @__PURE__ */ K(V, {
		icon: e,
		title: b("soon.title", { name: t }),
		text: b("soon.text")
	})] });
}
//#endregion
//#region src/views/AlertsPage.tsx
function st(e) {
	return /* @__PURE__ */ K(J, {
		icon: "bell",
		name: b("nav.alerts"),
		back: !1
	});
}
//#endregion
//#region src/views/AutoUpdatePage.tsx
function ct(e) {
	return /* @__PURE__ */ K(J, {
		icon: "refresh",
		name: b("nav.autoupdate"),
		back: !1
	});
}
//#endregion
//#region src/views/CleanupPage.tsx
function lt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "broom",
		name: b("nav.cleanup"),
		back: !1
	});
}
//#endregion
//#region src/views/ContainerPage.tsx
function ut(e) {
	return /* @__PURE__ */ K(J, {
		icon: "box",
		name: b("nav.containers"),
		back: !0
	});
}
//#endregion
//#region src/api/actions.ts
async function dt(e, t, n) {
	let r = await D.request("POST", `/containers/${encodeURIComponent(e)}/${t}`, { query: n?.timeoutSec === void 0 ? void 0 : { t: String(n.timeoutSec) } });
	if (r.status >= 400) {
		let e = "";
		try {
			e = r.json().message;
		} catch {}
		throw Error(e || `Docker answered ${r.status}`);
	}
	P.refresh();
}
async function ft(e, t = {}) {
	await D.delete(`/containers/${encodeURIComponent(e)}`, {
		force: t.force ? "1" : "0",
		v: t.volumes ? "1" : "0"
	}), P.refresh();
}
async function pt(e, t, n = 4) {
	let r = [], i = 0;
	return await Promise.all(Array.from({ length: Math.min(n, e.length) }, async () => {
		for (; i < e.length;) {
			let n = e[i++];
			try {
				await t(n);
			} catch (e) {
				r.push({
					id: n,
					message: e.message
				});
			}
		}
	})), r;
}
//#endregion
//#region src/api/stats.ts
function mt(e, t) {
	let n = e?.cpu_stats ?? t.precpu_stats;
	if (!n || !n.cpu_usage?.total_usage) return null;
	let r = t.cpu_stats.cpu_usage.total_usage - n.cpu_usage.total_usage, i = (t.cpu_stats.system_cpu_usage ?? 0) - (n.system_cpu_usage ?? 0);
	if (i <= 0 || r < 0) return null;
	let a = t.cpu_stats.online_cpus || t.cpu_stats.cpu_usage.percpu_usage?.length || 1;
	return r / i * a * 100;
}
function ht(e) {
	let t = e.usage ?? 0, n = e.stats?.inactive_file ?? e.stats?.total_inactive_file ?? e.stats?.cache ?? 0;
	return Math.max(0, t - n);
}
function gt(e, t) {
	let n = mt(e?.raw, t), r = ht(t.memory_stats), i = t.memory_stats.limit ?? 0, a = 0, o = 0;
	for (let e of Object.values(t.networks ?? {})) a += e.rx_bytes, o += e.tx_bytes;
	let s = 0, c = 0;
	for (let e of t.blkio_stats?.io_service_bytes_recursive ?? []) e.op.toLowerCase() === "read" ? s += e.value : e.op.toLowerCase() === "write" && (c += e.value);
	let l = Date.parse(t.read) || Date.now(), u = e ? (l - e.point.t) / 1e3 : 0;
	return {
		t: l,
		cpu: n ?? e?.point.cpu ?? 0,
		memUsed: r,
		memLimit: i,
		memPct: i > 0 ? r / i * 100 : 0,
		netRx: a,
		netTx: o,
		netRxRate: e && u > 0 ? Math.max(0, (a - e.point.netRx) / u) : 0,
		netTxRate: e && u > 0 ? Math.max(0, (o - e.point.netTx) / u) : 0,
		blkRead: s,
		blkWrite: c,
		pids: t.pids_stats?.current ?? 0
	};
}
var _t = 90, vt = 24, Y = /* @__PURE__ */ new Map(), X = 0;
function yt(e) {
	if (e.handle || X >= vt) return;
	X++;
	let t = new ve((t) => {
		let n = !e.prev, r = gt(e.prev, t);
		e.prev = {
			raw: t,
			point: r
		}, !n && (e.last = r, e.failures = 0, e.history.push(r), e.history.length > _t && e.history.shift(), e.listeners.forEach((e) => e()));
	}), n = () => {
		e.handle && (e.handle = void 0, X--, e.prev = void 0, e.listeners.size && ++e.failures <= 5 && (e.retry = setTimeout(() => yt(e), 3e3 * e.failures)));
	};
	e.handle = D.stream("GET", `/containers/${e.id}/stats`, { query: { stream: "true" } }, {
		onData: (e) => t.push(e),
		onEnd: n,
		onError: n
	});
}
function bt(e, t) {
	let n = Y.get(e);
	n || (n = {
		id: e,
		listeners: /* @__PURE__ */ new Set(),
		history: [],
		failures: 0
	}, Y.set(e, n)), n.listeners.add(t), n.listeners.size === 1 && (n.failures = 0, yt(n));
	let r = n;
	return () => {
		r.listeners.delete(t), r.listeners.size || (clearTimeout(r.retry), r.handle && (r.handle.close(), r.handle = void 0, X--), Y.delete(e));
	};
}
var xt = (e) => Y.get(e)?.history ?? [], St = (e) => Y.get(e)?.last;
//#endregion
//#region src/api/hooks.ts
function Ct(e, t = !0, n = 2e3) {
	let [, r] = c(0), i = u(0);
	return l(() => {
		if (e && t) return bt(e, () => {
			let e = Date.now();
			e - i.current >= n && (i.current = e, r((e) => e + 1));
		});
	}, [
		e,
		t,
		n
	]), !e || !t ? {
		last: void 0,
		history: []
	} : {
		last: St(e),
		history: xt(e)
	};
}
//#endregion
//#region src/settings.ts
var wt = "~/.config/linuxadmin/plugins/docker", Z = {
	settings: {
		stacksDir: "/opt/stacks",
		liveStats: !0,
		watchtowerImage: "nickfedor/watchtower"
	},
	registries: { registries: [] },
	alerts: { rules: [] },
	"templates-sources": { sources: [] }
}, Tt = (e) => `${wt}/${e}.json`, Et = (e) => JSON.parse(JSON.stringify(e));
async function Dt(e) {
	try {
		let t = await h().files.read(Tt(e)), n = JSON.parse(t);
		if (n && typeof n == "object" && !Array.isArray(n)) return {
			...Et(Z[e]),
			...n
		};
	} catch {}
	return Et(Z[e]);
}
var Ot = /* @__PURE__ */ new Map();
function kt(e, t) {
	let n = async () => {
		await h().files.write(Tt(e), JSON.stringify(t, null, 2) + "\n");
	}, r = (Ot.get(e) ?? Promise.resolve()).then(n, n);
	return Ot.set(e, r.catch(() => void 0)), r;
}
var Q = /* @__PURE__ */ new Map(), At = /* @__PURE__ */ new Set(), $ = /* @__PURE__ */ new Map(), jt = (e) => $.get(e)?.forEach((e) => e());
function Mt(e) {
	let t = f((t) => ($.has(e) || $.set(e, /* @__PURE__ */ new Set()), $.get(e).add(t), () => $.get(e).delete(t)), () => Q.get(e));
	return l(() => {
		Q.has(e) || At.has(e) || (At.add(e), Dt(e).then((t) => {
			Q.set(e, t), At.delete(e), jt(e);
		}));
	}, [e]), [
		t ?? Z[e],
		async (t) => {
			let n = {
				...Q.get(e) ?? Z[e],
				...t
			};
			Q.set(e, n), jt(e), await kt(e, n);
		},
		t !== void 0
	];
}
//#endregion
//#region src/ui/Charts.tsx
var Nt = (e) => e >= 90 ? "err" : e >= 70 ? "warn" : "ok";
function Pt({ value: e, max: t = 100, label: n }) {
	let r = t > 0 ? Math.max(0, Math.min(100, e / t * 100)) : 0;
	return /* @__PURE__ */ K("span", {
		className: `dk-meter dk-meter--${Nt(r)}`,
		role: "meter",
		"aria-label": n,
		"aria-valuemin": 0,
		"aria-valuemax": t,
		"aria-valuenow": Math.round(e),
		children: /* @__PURE__ */ K("i", { style: { width: `${r}%` } })
	});
}
function Ft({ values: e, max: t }) {
	return /* @__PURE__ */ K("div", {
		className: "dk-spark",
		children: /* @__PURE__ */ K(We, {
			values: e,
			height: 28,
			min: 0,
			max: t,
			color: "var(--acc)"
		})
	});
}
//#endregion
//#region src/ui/StatusDot.tsx
function It(e, t = !1) {
	if (t) return "err";
	switch (e) {
		case "running": return "ok";
		case "restarting": return "warn";
		case "paused": return "info";
		case "dead": return "err";
		default: return "n";
	}
}
function Lt({ state: e, unhealthy: t }) {
	return /* @__PURE__ */ K("span", {
		className: `dk-dot dk-dot--${It(e, t)}`,
		title: b(`state.${e}`)
	});
}
//#endregion
//#region src/ui/openUrl.ts
function Rt(e) {
	let t = h();
	if (typeof t.openExternal == "function") {
		t.openExternal(e);
		return;
	}
	window.open(e, "_blank", "noopener,noreferrer") || H.info(b("common.openedBlocked", { url: e }));
}
//#endregion
//#region src/views/ContainersPage.tsx
var zt = (e, t) => t === "all" ? !0 : t === "running" ? e.State === "running" : t === "stopped" ? Me(e) : je(e);
function Bt() {
	let { data: e, error: t, loading: n } = P.use(), r = it(), [i, a] = c("all"), [s, u] = c(/* @__PURE__ */ new Set()), [f, p] = c(/* @__PURE__ */ new Set()), [m, h] = c(null), [ee, g] = c(!1), [te] = Mt("settings"), ne = Te.use().data?.info.NCPU || 1, _ = e ?? [], v = Ie(_), y = d(() => new Map(_.map((e) => [e.Id, e])), [_]);
	l(() => {
		e && u((e) => {
			let t = [...e].filter((e) => y.has(e));
			return t.length === e.size ? e : new Set(t);
		});
	}, [e, y]);
	let re = d(() => _.filter((e) => zt(e, i) && Le(e, r)), [
		_,
		i,
		r
	]), ie = d(() => Pe(re), [re]), x = (e, t) => p((n) => {
		let r = new Set(n);
		return e.forEach((e) => t ? r.add(e) : r.delete(e)), r;
	}), ae = async (e, t) => {
		let n = F(e);
		x([e.Id], !0);
		try {
			await dt(e.Id, t), H.ok(b(`containers.done.${t}`, { name: n }));
		} catch (e) {
			H.err(b(`containers.fail.${t}`, { name: n }), e.message);
		} finally {
			x([e.Id], !1);
		}
	}, S = async (e, t) => {
		let n = e.map((e) => e.Id);
		x(n, !0);
		let r = await pt(n, (e) => dt(e, t));
		x(n, !1), r.length ? H.err(b("containers.bulkFail", {
			failed: r.length,
			total: n.length
		}), r[0].message) : H.ok(b(`containers.bulkDone.${t}`, { n: n.length })), t === "stop" && u(/* @__PURE__ */ new Set());
	}, oe = async () => {
		let e = m ?? [], t = e.map((e) => e.Id);
		x(t, !0);
		let n = await pt(t, (e) => ft(e, {
			force: !0,
			volumes: ee
		}));
		x(t, !1), h(null), g(!1), u(/* @__PURE__ */ new Set()), n.length ? H.err(b("containers.bulkFail", {
			failed: n.length,
			total: t.length
		}), n[0].message) : H.ok(e.length === 1 ? b("containers.done.remove", { name: F(e[0]) }) : b("containers.bulkDone.remove", { n: e.length }));
	}, se = (e, t) => u((n) => {
		let r = new Set(n);
		return t ? r.add(e) : r.delete(e), r;
	}), ce = v.total ? [
		v.running && b("containers.sub.running", { n: v.running }),
		v.restarting && b("containers.sub.restarting", { n: v.restarting }),
		v.paused && b("containers.sub.paused", { n: v.paused }),
		v.stopped && b("containers.sub.stopped", { n: v.stopped })
	].filter(Boolean).join(", ") : e ? b("containers.sub.none") : "", C = /* @__PURE__ */ K(ot, {
		icon: "box",
		title: b("containers.title"),
		subtitle: ce,
		actions: /* @__PURE__ */ K(L, {
			variant: "primary",
			icon: "plus",
			onClick: () => G({ view: "create" }),
			children: b("containers.new")
		})
	});
	if (!e && t) return /* @__PURE__ */ q(o, { children: [C, /* @__PURE__ */ K(at, {
		error: t,
		onRetry: () => {
			fe(), P.refresh();
		}
	})] });
	if (!e && n) return /* @__PURE__ */ q(o, { children: [C, /* @__PURE__ */ K("div", {
		className: "dk-grid",
		children: Array.from({ length: 6 }, (e, t) => /* @__PURE__ */ K(Ve, {
			height: 132,
			style: { borderRadius: 18 }
		}, t))
	})] });
	let le = [
		["all", v.total],
		["running", v.running],
		["stopped", v.stopped],
		["unhealthy", v.problems]
	], w = [...s].map((e) => y.get(e)).filter((e) => !!e);
	return /* @__PURE__ */ q(o, { children: [
		C,
		t && /* @__PURE__ */ K("p", {
			className: "dk-muted dk-stale",
			children: b("containers.stale")
		}),
		v.total > 0 && /* @__PURE__ */ K("div", {
			className: "dk-chips",
			role: "group",
			"aria-label": b("containers.title"),
			children: le.map(([e, t]) => /* @__PURE__ */ K(Be, {
				pressed: i === e,
				count: t,
				onClick: () => a(e),
				children: b(`containers.filter.${e}`)
			}, e))
		}),
		v.total === 0 ? /* @__PURE__ */ K(V, {
			icon: "box",
			hue: "file",
			title: b("containers.empty.title"),
			text: b("containers.empty.text"),
			action: /* @__PURE__ */ q("div", {
				className: "dk-ph-act",
				style: { margin: 0 },
				children: [/* @__PURE__ */ K(L, {
					variant: "primary",
					icon: "plus",
					onClick: () => G({ view: "create" }),
					children: b("containers.new")
				}), /* @__PURE__ */ K(L, {
					icon: "store",
					onClick: () => G({ view: "templates" }, { root: !0 }),
					children: b("containers.empty.template")
				})]
			})
		}) : ie.length === 0 ? /* @__PURE__ */ K(V, {
			icon: "search",
			hue: "file",
			title: b("containers.noMatch.title"),
			text: b("containers.noMatch.text")
		}) : /* @__PURE__ */ K("div", {
			className: "dk-groups",
			children: ie.map((e) => /* @__PURE__ */ K(Vt, {
				group: e,
				selected: s,
				busy: f,
				liveStats: te.liveStats,
				ncpu: ne,
				onToggle: se,
				onSelectGroup: (t) => u((n) => {
					let r = new Set(n);
					return e.containers.forEach((e) => t ? r.add(e.Id) : r.delete(e.Id)), r;
				}),
				onAction: ae,
				onRestartStack: () => S(e.containers, "restart")
			}, e.name || "\0standalone"))
		}),
		w.length > 0 && /* @__PURE__ */ q("div", {
			className: "dk-bulk",
			role: "toolbar",
			"aria-label": b("containers.selected", { n: w.length }),
			children: [
				/* @__PURE__ */ K("b", { children: b("containers.selected", { n: w.length }) }),
				/* @__PURE__ */ q("button", {
					type: "button",
					onClick: () => S(w, "start"),
					children: [/* @__PURE__ */ K(z, { name: "play" }), b("common.start")]
				}),
				/* @__PURE__ */ q("button", {
					type: "button",
					onClick: () => S(w, "stop"),
					children: [/* @__PURE__ */ K(z, { name: "stop" }), b("common.stop")]
				}),
				/* @__PURE__ */ q("button", {
					type: "button",
					onClick: () => S(w, "restart"),
					children: [/* @__PURE__ */ K(z, { name: "refresh" }), b("common.restart")]
				}),
				/* @__PURE__ */ q("button", {
					type: "button",
					onClick: () => h(w),
					children: [/* @__PURE__ */ K(z, { name: "trash" }), b("common.remove")]
				}),
				/* @__PURE__ */ K("button", {
					type: "button",
					"aria-label": b("containers.clearSelection"),
					title: b("containers.clearSelection"),
					onClick: () => u(/* @__PURE__ */ new Set()),
					children: /* @__PURE__ */ K(z, { name: "close" })
				})
			]
		}),
		/* @__PURE__ */ q(Ue, {
			open: !!m,
			onClose: () => {
				h(null), g(!1);
			},
			onConfirm: oe,
			title: m?.length === 1 ? b("containers.removeTitle", { name: F(m[0]) }) : b("containers.removeMany", { n: m?.length ?? 0 }),
			description: b("containers.removeDesc"),
			confirmLabel: b("common.remove"),
			confirmText: m?.length === 1 ? F(m[0]) : String(m?.length ?? ""),
			icon: "trash",
			children: [/* @__PURE__ */ q("div", {
				className: "dk-rm-list",
				children: [(m ?? []).slice(0, 12).map((e) => /* @__PURE__ */ K("div", { children: F(e) }, e.Id)), (m?.length ?? 0) > 12 && /* @__PURE__ */ K("div", { children: "…" })]
			}), /* @__PURE__ */ K(B, {
				checked: ee,
				onChange: g,
				label: b("containers.removeVolumes")
			})]
		})
	] });
}
function Vt({ group: e, selected: t, busy: n, liveStats: r, ncpu: i, onToggle: a, onSelectGroup: o, onAction: s, onRestartStack: c }) {
	let l = e.containers.every((e) => t.has(e.Id));
	return /* @__PURE__ */ q("section", {
		className: "dk-stack",
		children: [/* @__PURE__ */ q("div", {
			className: "dk-stack-h",
			children: [
				/* @__PURE__ */ q("span", {
					className: "dk-stack-n",
					children: [/* @__PURE__ */ K(z, { name: e.name ? "layers" : "box" }), e.name || b("containers.standalone")]
				}),
				/* @__PURE__ */ K("span", {
					className: "dk-muted",
					children: b("containers.stackRunning", {
						running: e.running,
						total: e.containers.length
					})
				}),
				/* @__PURE__ */ q("span", {
					className: "dk-stack-act",
					children: [
						/* @__PURE__ */ K(B, {
							checked: l,
							indeterminate: !l && e.containers.some((e) => t.has(e.Id)),
							onChange: o,
							label: b("containers.selectAll")
						}),
						e.name && /* @__PURE__ */ K(L, {
							size: "sm",
							variant: "ghost",
							icon: "edit",
							onClick: () => G({
								view: "stack",
								name: e.name
							}),
							children: b("containers.openStack")
						}),
						e.name && /* @__PURE__ */ K(L, {
							size: "sm",
							variant: "ghost",
							icon: "refresh",
							onClick: c,
							children: b("containers.restartStack")
						})
					]
				})
			]
		}), /* @__PURE__ */ K("div", {
			className: "dk-grid",
			children: e.containers.map((e) => /* @__PURE__ */ K(Ht, {
				c: e,
				selected: t.has(e.Id),
				busy: n.has(e.Id),
				live: r,
				ncpu: i,
				onToggle: (t) => a(e.Id, t),
				onAction: (t) => s(e, t)
			}, e.Id))
		})]
	});
}
function Ht({ c: e, selected: t, busy: n, live: r, ncpu: i, onToggle: a, onAction: o }) {
	let s = F(e), c = e.State === "running", { last: l, history: u } = Ct(e.Id, c && r), d = je(e), f = Fe(e).filter((e) => e.proto === "tcp"), p = () => G({
		view: "container",
		id: e.Id
	}), m = (e) => e.stopPropagation();
	return /* @__PURE__ */ q("div", {
		className: `dk-cc${d ? " dk-cc--bad" : ""}${t ? " dk-cc--sel" : ""}`,
		role: "link",
		tabIndex: 0,
		"aria-label": b("containers.openContainer", { name: s }),
		onClick: p,
		onKeyDown: (e) => {
			e.key === "Enter" && e.target === e.currentTarget && p();
		},
		children: [
			/* @__PURE__ */ q("div", {
				className: "dk-cc-t",
				children: [
					/* @__PURE__ */ K("span", {
						className: "dk-cc-ck",
						onClick: m,
						children: /* @__PURE__ */ K(B, {
							checked: t,
							onChange: a,
							"aria-label": b("containers.select", { name: s })
						})
					}),
					/* @__PURE__ */ K(Lt, {
						state: e.State,
						unhealthy: Ae(e) === "unhealthy"
					}),
					/* @__PURE__ */ q("div", {
						className: "dk-cc-tx",
						children: [/* @__PURE__ */ K("b", {
							title: s,
							children: s
						}), /* @__PURE__ */ K("span", {
							className: "dk-cc-img",
							title: e.Image,
							children: ke(e.Image)
						})]
					})
				]
			}),
			/* @__PURE__ */ q("div", {
				className: "dk-cc-m",
				children: [/* @__PURE__ */ q("div", { children: [/* @__PURE__ */ q("small", { children: [b("containers.cpu"), /* @__PURE__ */ K("span", { children: l ? Oe(l.cpu) : "–" })] }), /* @__PURE__ */ K(Pt, {
					value: l?.cpu ?? 0,
					max: i * 100,
					label: b("containers.cpu")
				})] }), /* @__PURE__ */ q("div", { children: [/* @__PURE__ */ q("small", { children: [b("containers.memory"), /* @__PURE__ */ K("span", { children: l ? De(l.memUsed) : "–" })] }), /* @__PURE__ */ K(Pt, {
					value: l?.memPct ?? 0,
					max: 100,
					label: b("containers.memory")
				})] })]
			}),
			c && r && /* @__PURE__ */ K(Ft, { values: u.map((e) => e.cpu) }),
			/* @__PURE__ */ K("span", {
				className: "dk-cc-st",
				title: e.Status,
				children: e.Status
			}),
			/* @__PURE__ */ q("div", {
				className: "dk-cc-f",
				children: [/* @__PURE__ */ q("div", {
					className: "dk-cc-ports",
					children: [f.slice(0, 3).map((e) => /* @__PURE__ */ q("button", {
						type: "button",
						className: "dk-port",
						title: b("containers.port", { port: e.host }),
						onClick: (t) => {
							m(t), Rt(Re(e));
						},
						children: [e.host, /* @__PURE__ */ K(z, { name: "externallink" })]
					}, e.host)), f.length > 3 && /* @__PURE__ */ q("span", {
						className: "dk-muted",
						children: ["+", f.length - 3]
					})]
				}), /* @__PURE__ */ q("div", {
					className: "dk-cc-act",
					onClick: m,
					children: [e.State === "running" || e.State === "restarting" || e.State === "paused" ? /* @__PURE__ */ K(R, {
						icon: "stop",
						label: b("common.stop"),
						loading: n,
						onClick: () => o("stop")
					}) : /* @__PURE__ */ K(R, {
						icon: "play",
						label: b("common.start"),
						loading: n,
						onClick: () => o("start")
					}), /* @__PURE__ */ K(R, {
						icon: "refresh",
						label: b("common.restart"),
						disabled: n,
						onClick: () => o("restart")
					})]
				})]
			})
		]
	});
}
//#endregion
//#region src/views/CreatePage.tsx
function Ut(e) {
	return /* @__PURE__ */ K(J, {
		icon: "box",
		name: b("containers.new"),
		back: !0
	});
}
//#endregion
//#region src/views/ImagesPage.tsx
function Wt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "image",
		name: b("nav.images"),
		back: !1
	});
}
//#endregion
//#region src/views/NetworksPage.tsx
function Gt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "net",
		name: b("nav.networks"),
		back: !1
	});
}
//#endregion
//#region src/views/RegistriesPage.tsx
function Kt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "key",
		name: b("nav.registries"),
		back: !1
	});
}
//#endregion
//#region src/views/StackPage.tsx
function qt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "layers",
		name: b("nav.stacks"),
		back: !0
	});
}
//#endregion
//#region src/views/StacksPage.tsx
function Jt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "layers",
		name: b("nav.stacks"),
		back: !1
	});
}
//#endregion
//#region src/views/TemplatePage.tsx
function Yt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "store",
		name: b("nav.templates"),
		back: !0
	});
}
//#endregion
//#region src/views/TemplatesPage.tsx
function Xt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "store",
		name: b("nav.templates"),
		back: !1
	});
}
//#endregion
//#region src/views/VolumesPage.tsx
function Zt(e) {
	return /* @__PURE__ */ K(J, {
		icon: "database",
		name: b("nav.volumes"),
		back: !1
	});
}
//#endregion
//#region src/views/registry.tsx
function Qt({ route: e }) {
	switch (e.view) {
		case "containers": return /* @__PURE__ */ K(Bt, {});
		case "container": return /* @__PURE__ */ K(ut, {
			id: e.id,
			tab: e.tab
		});
		case "stacks": return /* @__PURE__ */ K(Jt, {});
		case "stack": return /* @__PURE__ */ K(qt, { name: e.name });
		case "create": return /* @__PURE__ */ K(Ut, {
			from: e.from,
			image: e.image
		});
		case "templates": return /* @__PURE__ */ K(Xt, {});
		case "template": return /* @__PURE__ */ K(Yt, { id: e.id });
		case "images": return /* @__PURE__ */ K(Wt, {});
		case "volumes": return /* @__PURE__ */ K(Zt, {});
		case "networks": return /* @__PURE__ */ K(Gt, {});
		case "registries": return /* @__PURE__ */ K(Kt, {});
		case "cleanup": return /* @__PURE__ */ K(lt, {});
		case "autoupdate": return /* @__PURE__ */ K(ct, {});
		case "alerts": return /* @__PURE__ */ K(st, {});
	}
}
//#endregion
//#region src/shell/App.tsx
var $t = [
	{
		label: "nav.workloads",
		items: [
			"containers",
			"stacks",
			"templates"
		]
	},
	{
		label: "nav.resources",
		items: [
			"images",
			"volumes",
			"networks",
			"registries"
		]
	},
	{
		label: "nav.tools",
		items: [
			"cleanup",
			"autoupdate",
			"alerts"
		]
	}
], en = [
	"containers",
	"stacks",
	"images"
], tn = ["registries"];
function nn() {
	let e = $e(), t = Ge(e), n = it(), r = P.use(), i = Se.use(), a = Ce.use(), o = we.use(), s = Te.use(), c = {};
	r.data && (c.containers = r.data.length, c.stacks = Pe(r.data).filter((e) => e.name).length), i.data && (c.images = i.data.length), a.data && (c.volumes = a.data.length), o.data && (c.networks = o.data.length);
	let l = (e) => {
		rt(e), e && !en.includes(t) && G({ view: "containers" }, { root: !0 });
	}, u = !r.data && r.error && !tn.includes(t);
	return /* @__PURE__ */ K("div", {
		className: "dk-root hue-file",
		children: /* @__PURE__ */ q("div", {
			className: "dk-shell",
			children: [/* @__PURE__ */ q("nav", {
				className: "dk-nav",
				"aria-label": b("nav.aria"),
				children: [$t.map((e) => /* @__PURE__ */ q("div", {
					style: { display: "contents" },
					children: [/* @__PURE__ */ K("div", {
						className: "dk-nav-gl",
						children: b(e.label)
					}), e.items.map((e) => /* @__PURE__ */ q("button", {
						type: "button",
						className: "dk-nav-it",
						"aria-current": t === e ? "page" : void 0,
						onClick: () => G({ view: e }, { root: !0 }),
						children: [
							/* @__PURE__ */ K(z, { name: le[e] }),
							b(`nav.${e}`),
							c[e] !== void 0 && /* @__PURE__ */ K("b", { children: c[e] })
						]
					}, e))]
				}, e.label)), s.data && /* @__PURE__ */ q("div", {
					className: "dk-nav-ft",
					children: [/* @__PURE__ */ K("span", { className: "dk-dot dk-dot--ok" }), b("shell.engine", { version: s.data.version.Version })]
				})]
			}), /* @__PURE__ */ q("div", {
				className: "dk-main",
				children: [/* @__PURE__ */ K("div", {
					className: "dk-top",
					children: /* @__PURE__ */ K(ze, {
						fieldClassName: "dk-search",
						icon: "search",
						value: n,
						placeholder: b("shell.search"),
						"aria-label": b("shell.search"),
						onChange: (e) => l(e.target.value),
						end: n ? /* @__PURE__ */ K(R, {
							icon: "close",
							label: b("common.close"),
							size: "sm",
							onClick: () => rt("")
						}) : void 0
					})
				}), /* @__PURE__ */ K("div", {
					className: "dk-view",
					children: u ? /* @__PURE__ */ K(at, {
						error: r.error,
						onRetry: () => {
							fe(), P.refresh();
						}
					}) : /* @__PURE__ */ K(Qt, { route: e }, JSON.stringify(e))
				})]
			})]
		})
	});
}
//#endregion
//#region src/views/ContainersWidget.tsx
function rn() {
	let { data: e, error: t } = P.use(), n = e ? Ie(e) : void 0, r = t && !e ? t.message : n ? n.problems ? b("containers.widget.problems", { n: n.problems }) : n.total ? b("containers.widget.allWell") : b("containers.sub.none") : "";
	return /* @__PURE__ */ q("div", { children: [/* @__PURE__ */ K(He, {
		hue: "file",
		icon: "box",
		label: b("containers.title"),
		value: n ? `${n.running}/${n.total}` : t ? "–" : "…",
		unit: n ? ` ${b("containers.widget.running")}` : void 0,
		sub: r,
		onClick: () => h().open("docker")
	}), /* @__PURE__ */ K("div", {
		className: "dk-w-open",
		children: /* @__PURE__ */ K(L, {
			size: "sm",
			icon: "externallink",
			onClick: () => h().open("docker"),
			children: b("containers.widget.open")
		})
	})] });
}
//#endregion
//#region src/index.ts
function an() {
	return s(V, {
		icon: "alert",
		hue: "svc",
		title: b("shell.oldSdk.title"),
		text: b("shell.oldSdk.text")
	});
}
function on(e) {
	m(e), r(e.react), ie(), ce(), C();
	let t = e.version >= 3;
	e.registerPage("docker", t ? nn : an), e.registerWidget({
		id: "containers",
		render: t ? rn : an
	});
}
//#endregion
export { on as default };
