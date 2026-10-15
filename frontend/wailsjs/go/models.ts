export namespace app {
	
	export class Boite {
	    mac: string;
	    ssid: string;
	    nom: string;
	    firmware?: string;
	    stockage_total?: number;
	    stockage_libre?: number;
	    derniere_sync?: number;
	    wifi_maison?: string;
	    rapport?: playlist.Report;
	
	    static createFrom(source: any = {}) {
	        return new Boite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mac = source["mac"];
	        this.ssid = source["ssid"];
	        this.nom = source["nom"];
	        this.firmware = source["firmware"];
	        this.stockage_total = source["stockage_total"];
	        this.stockage_libre = source["stockage_libre"];
	        this.derniere_sync = source["derniere_sync"];
	        this.wifi_maison = source["wifi_maison"];
	        this.rapport = this.convertValues(source["rapport"], playlist.Report);
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
	export class EcouteVue {
	    uuid: string;
	    titre: string;
	    serie: string;
	    debut: number;
	    minutes: number;
	    duree_s: number;
	
	    static createFrom(source: any = {}) {
	        return new EcouteVue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.titre = source["titre"];
	        this.serie = source["serie"];
	        this.debut = source["debut"];
	        this.minutes = source["minutes"];
	        this.duree_s = source["duree_s"];
	    }
	}
	export class DernieresEcoutes {
	    releve: number;
	    ecoutes: EcouteVue[];
	
	    static createFrom(source: any = {}) {
	        return new DernieresEcoutes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.releve = source["releve"];
	        this.ecoutes = this.convertValues(source["ecoutes"], EcouteVue);
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
	
	export class EstimationStockage {
	    ajoutMo: number;
	    retraitMo: number;
	
	    static createFrom(source: any = {}) {
	        return new EstimationStockage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ajoutMo = source["ajoutMo"];
	        this.retraitMo = source["retraitMo"];
	    }
	}
	export class EtatConnexion {
	    joignable: boolean;
	    mac?: string;
	    batterie?: device.Batterie;
	    occupee?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EtatConnexion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.joignable = source["joignable"];
	        this.mac = source["mac"];
	        this.batterie = this.convertValues(source["batterie"], device.Batterie);
	        this.occupee = source["occupee"];
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
	export class EtatSauvegarde {
	    disponible: boolean;
	    date: number;
	    fichiers: number;
	    octets: number;
	
	    static createFrom(source: any = {}) {
	        return new EtatSauvegarde(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disponible = source["disponible"];
	        this.date = source["date"];
	        this.fichiers = source["fichiers"];
	        this.octets = source["octets"];
	    }
	}
	export class EtatsUUID {
	    surBoite: string[];
	    attenteAjout: string[];
	    attenteSuppr: string[];
	    favoris: string[];
	
	    static createFrom(source: any = {}) {
	        return new EtatsUUID(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.surBoite = source["surBoite"];
	        this.attenteAjout = source["attenteAjout"];
	        this.attenteSuppr = source["attenteSuppr"];
	        this.favoris = source["favoris"];
	    }
	}
	export class HistoirePerso {
	    uuid: string;
	    titre: string;
	    ext: string;
	    taille: number;
	    cover: boolean;
	    ajout: number;
	    duree: number;
	    img?: string;
	    referencee: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoirePerso(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.titre = source["titre"];
	        this.ext = source["ext"];
	        this.taille = source["taille"];
	        this.cover = source["cover"];
	        this.ajout = source["ajout"];
	        this.duree = source["duree"];
	        this.img = source["img"];
	        this.referencee = source["referencee"];
	    }
	}
	export class RadioDispo {
	    uuid: string;
	    titre: string;
	    logo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RadioDispo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.titre = source["titre"];
	        this.logo = source["logo"];
	    }
	}
	export class RadiosResult {
	    conteneurUuid: string;
	    conteneurLogo: boolean;
	    stations: RadioDispo[];
	
	    static createFrom(source: any = {}) {
	        return new RadiosResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.conteneurUuid = source["conteneurUuid"];
	        this.conteneurLogo = source["conteneurLogo"];
	        this.stations = this.convertValues(source["stations"], RadioDispo);
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
	export class ResultatMedia {
	    ok: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ResultatMedia(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	    }
	}
	export class SerieNouveaute {
	    uuid: string;
	    title: string;
	    src: string;
	    srcId: string;
	    img: string;
	    nbNouv: number;
	
	    static createFrom(source: any = {}) {
	        return new SerieNouveaute(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.title = source["title"];
	        this.src = source["src"];
	        this.srcId = source["srcId"];
	        this.img = source["img"];
	        this.nbNouv = source["nbNouv"];
	    }
	}
	export class Stockage {
	    total: number;
	    libre: number;
	
	    static createFrom(source: any = {}) {
	        return new Stockage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.libre = source["libre"];
	    }
	}

}

export namespace device {
	
	export class Batterie {
	    niveau: number;
	    en_charge: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Batterie(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.niveau = source["niveau"];
	        this.en_charge = source["en_charge"];
	    }
	}
	export class Plage {
	    debutMin: number;
	    finMin: number;
	
	    static createFrom(source: any = {}) {
	        return new Plage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.debutMin = source["debutMin"];
	        this.finMin = source["finMin"];
	    }
	}
	export class Horaires {
	    jours: Plage[];
	    nePasCouper: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Horaires(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jours = this.convertValues(source["jours"], Plage);
	        this.nePasCouper = source["nePasCouper"];
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

export namespace itunes {
	
	export class Episode {
	    id: string;
	    uuid: string;
	    title: string;
	    image: string;
	    description: string;
	    duration: number;
	    published: string;
	    audioSrc: string;
	    playable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Episode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.uuid = source["uuid"];
	        this.title = source["title"];
	        this.image = source["image"];
	        this.description = source["description"];
	        this.duration = source["duration"];
	        this.published = source["published"];
	        this.audioSrc = source["audioSrc"];
	        this.playable = source["playable"];
	    }
	}
	export class Feed {
	    description: string;
	    image: string;
	    episodes: Episode[];
	
	    static createFrom(source: any = {}) {
	        return new Feed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.description = source["description"];
	        this.image = source["image"];
	        this.episodes = this.convertValues(source["episodes"], Episode);
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
	export class Podcast {
	    id: string;
	    uuid: string;
	    title: string;
	    artist: string;
	    image: string;
	    category: string;
	    explicit: boolean;
	    nbEpisode: number;
	    episodeMatch?: string;
	
	    static createFrom(source: any = {}) {
	        return new Podcast(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.uuid = source["uuid"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.image = source["image"];
	        this.category = source["category"];
	        this.explicit = source["explicit"];
	        this.nbEpisode = source["nbEpisode"];
	        this.episodeMatch = source["episodeMatch"];
	    }
	}

}

export namespace playlist {
	
	export class Node {
	    uuid: string;
	    title: string;
	    child?: Node[];
	    add_time?: number;
	    limit_time?: number;
	    favorite?: number;
	    webradio?: number;
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.title = source["title"];
	        this.child = this.convertValues(source["child"], Node);
	        this.add_time = source["add_time"];
	        this.limit_time = source["limit_time"];
	        this.favorite = source["favorite"];
	        this.webradio = source["webradio"];
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
	export class Report {
	    categories: number;
	    dossiers: number;
	    histoires: number;
	    total: number;
	    favori_titre: string;
	    octets: number;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.categories = source["categories"];
	        this.dossiers = source["dossiers"];
	        this.histoires = source["histoires"];
	        this.total = source["total"];
	        this.favori_titre = source["favori_titre"];
	        this.octets = source["octets"];
	    }
	}

}

