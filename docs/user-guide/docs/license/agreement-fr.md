# CONTRAT DE LICENCE LOGICIELLE — SENHUB AGENT

*Version 1.0 — complément aux Conditions Générales de Vente de Sensor Factory.
Applicable au logiciel SenHub Agent, toutes versions.*


!!! info "Version qui fait foi"
    Ce texte est la version française du contrat, **seule version qui fait
    foi** entre les Parties. Une [traduction anglaise de courtoisie](agreement-en.md)
    est disponible. La version imprimable est téléchargeable en
    [PDF](contrat-de-licence-senhub-agent.pdf).

---

## Article 1 — Objet et place du contrat

1.1 — Le présent contrat (le « **Contrat de Licence** ») régit la concession, par
**SENSOR FACTORY SAS** (ci-après « Sensor Factory »), du droit d'utiliser les
composants sous licence du logiciel **SenHub Agent** (le « **Logiciel** »).

1.2 — Il complète les Conditions Générales de Vente de Sensor Factory en vigueur à
la date de la commande (les « **CGV** »), qui demeurent le socle de la relation
commerciale et régissent tout ce que le présent contrat ne traite pas expressément.

1.3 — **Formation.** L'acceptation du Devis par le Client, par signature ou par
l'émission d'un bon de commande y faisant référence, emporte acceptation du présent
Contrat de Licence et des CGV. Le Devis porte le niveau souscrit et les éléments de
dimensionnement. Les Parties peuvent convenir de Conditions particulières ; chacune
peut en demander l'établissement avant l'acceptation du Devis.

1.4 — **Ordre de préséance.** Pour ce que chacun régit, les documents contractuels
s'appliquent dans l'ordre suivant : (1) les Conditions particulières, lorsque les
Parties en signent ; (2) le présent Contrat de Licence, pour la concession du droit
d'usage du Logiciel, son périmètre, sa durée et son contrôle ; (3) le Devis accepté,
pour le niveau souscrit, le dimensionnement et le prix ; (4) les CGV. En cas de
contradiction, le document de rang supérieur prévaut.

**Par exception**, les stipulations des CGV visées à l'article 17.2 prévalent sur
tout autre document, quel que soit son rang, y compris les Conditions particulières
et le Devis, sauf renonciation **expresse** de Sensor Factory, écrite, visant
nommément la stipulation à laquelle il est dérogé et signée par un mandataire
social.

## Article 2 — Structure du Logiciel

2.1 — Le Logiciel se compose de deux ensembles dont le régime juridique diffère.
Cette distinction commande la lecture de tout ce qui suit.

2.2 — **Le cœur ouvert.** Une partie du Logiciel, décrite dans sa documentation, est
publiée sous licence **Apache 2.0** et son code source est public. Le Client en
dispose selon les termes de cette licence, indépendamment du présent Contrat de
Licence.

Cette licence est acquise, pour chaque version, à la version dans laquelle le
composant a été publié : ce qui a été publié sous Apache 2.0 le demeure. Sensor
Factory ne s'engage en revanche à aucune répartition déterminée entre le cœur ouvert
et les composants sous licence dans les versions à venir, et demeure libre de
publier un composant nouveau sous l'un ou l'autre régime.

2.3 — **Les sondes sous licence.** Certains types de sondes, portant sur des
équipements et des logiciels d'éditeurs tiers, ne sont pas couverts par la licence
Apache 2.0. Leur usage est concédé par le présent Contrat de Licence, contre
rémunération, pour la durée qu'il fixe.

2.4 — Il en résulte une conséquence que le Client doit connaître avant de signer :
l'expiration ou la résiliation du présent Contrat de Licence ne prive le Client
d'aucun des droits qu'il tient de la licence Apache 2.0 sur le cœur ouvert. Elle
porte sur les seules Sondes sous Licence. Le fonctionnement correspondant du
Logiciel est décrit à l'article 6.6.

## Article 3 — Définitions

Les termes définis par les CGV conservent ici le même sens. Sont en outre définis :

- **Agent** : une instance du Logiciel installée sur une machine, physique ou
  virtuelle, ou exécutée dans un conteneur.
- **Parc** : l'ensemble des Agents exploités par le Client et par les entités
  qu'il contrôle au sens de l'article L.233-3 du code de commerce, sous réserve de
  l'article 15.4. Le Client répond du respect du présent contrat par ces entités
  **comme du sien propre**, et se porte fort de leur collaboration à la
  vérification prévue à l'article 13.3.
- **Périmètre Souscrit** : les éléments de dimensionnement figurant au Devis
  accepté, notamment le nombre d'Agents, dans la limite desquels le droit d'usage
  est concédé. À défaut de mention chiffrée au Devis, le Périmètre Souscrit est
  celui que le Client a déclaré lors de la commande et, à défaut de déclaration, le
  nombre d'Agents en service dans le Parc à la date de prise d'effet du contrat.
- **Clé d'Agent** : l'identifiant que chaque Agent produit lui-même à sa première
  exécution. Elle n'est jamais saisie par le Client ni attribuée par Sensor Factory.
- **Jeton de Licence** : le fichier signé que Sensor Factory remet au Client et qui
  porte le niveau concédé, le périmètre des sondes autorisées, le titulaire et la
  date d'échéance.
- **Sonde sous Licence** : un type de sonde dont l'usage requiert un Jeton de
  Licence valide.
- **Sonde Libre** : tout autre type de sonde, utilisable sans Jeton de Licence et
  sans contrepartie financière.

## Article 4 — Concession

4.1 — Sensor Factory concède au Client, pour la durée fixée à l'article 7 et sous
réserve du paiement du prix convenu, un droit d'utiliser les Sondes sous Licence
correspondant au niveau souscrit.

4.2 — Ce droit est **non exclusif**, **non cessible** et **non susceptible de
sous-licence**. Il est concédé pour les besoins propres du Client, à l'exclusion de
toute exploitation pour le compte de tiers, qu'elle soit rémunérée ou non.

4.3 — **Périmètre.** Le droit s'exerce sur le Parc dans la limite du Périmètre
Souscrit.

Les modalités par lesquelles ce droit se met en œuvre sur chaque Agent, qu'il
s'agisse de déclaration, d'activation ou de vérification, sont celles de la version
du Logiciel que le Client déploie. L'article 6 les expose dans leur état à la date
de souscription et l'article 6.8 en encadre l'évolution. Aucune de ces modalités ne
modifie le Périmètre Souscrit, qu'elle en facilite ou non le contrôle.

**Le dépassement** de chacun des éléments quantitatifs du Périmètre Souscrit, pris
séparément, ne donne pas lieu à régularisation dans la limite de **cinq pour cent
(5 %)** de la valeur souscrite pour cet élément, arrondie à l'unité supérieure.
Au-delà, le Client en informe Sensor Factory dans les trente (30) jours et les
Parties conviennent de l'extension correspondante, facturée au tarif
public en vigueur et au prorata de la période restant à courir. À défaut
d'information dans ce délai, l'article 13.4 s'applique.

4.4 — **À chaque renouvellement**, le Client déclare par écrit à Sensor Factory les
éléments constitutifs du Périmètre Souscrit pour la période suivante. Cette
déclaration conditionne le renouvellement et sert de base au prix. Une déclaration
inexacte produit les effets de l'article 13.

4.5 — **Le Client s'interdit** de contourner, neutraliser ou retirer les mécanismes
décrits à l'article 6, de communiquer son Jeton de Licence à un tiers, de proposer
les Sondes sous Licence dans une offre de service à des tiers, et de décompiler les
composants sous licence hors des cas où la loi l'autorise impérativement.
L'article L.122-6-1 du code de la propriété intellectuelle demeure réservé.

4.6 — Le Client s'interdit également de supprimer ou d'altérer les mentions de
propriété, de licence et de version portées par le Logiciel.

4.7 — Le Client s'interdit de publier des résultats d'essais comparatifs portant sur
les Sondes sous Licence sans l'accord écrit préalable de Sensor Factory. Cet accord
ne peut être refusé sans motif. Sensor Factory dispose d'un délai de trente (30)
jours pour se prononcer ; **son silence à l'expiration de ce délai vaut accord**.

## Article 5 — Niveaux et périmètre

5.1 — Trois niveaux existent. Le niveau souscrit figure dans le Devis accepté et se
retrouve dans le Jeton de Licence.

- **Gratuit** : toutes les Sondes Libres. Aucun Jeton de Licence, aucune
  contrepartie financière, aucune limite de durée ni de nombre d'Agents. Ce niveau
  ne requiert aucune souscription ; le présent contrat ne le régit que lorsqu'un
  niveau payant est par ailleurs souscrit.
- **Pro** : les Sondes Libres, et les Sondes sous Licence que la documentation
  produit désigne comme telles à la date de souscription.
- **Entreprise** : les Sondes Libres, les Sondes sous Licence, et celles que Sensor
  Factory viendrait à publier pendant la durée du contrat, à l'exception des modules
  que Sensor Factory désigne, lors de leur publication, comme faisant l'objet d'une
  tarification distincte, notamment lorsqu'ils dépendent d'un composant ou d'une
  infrastructure tierce donnant lieu à redevance.

5.2 — Le périmètre concédé ne se lit pas dans le présent Contrat de Licence, qui ne
le fige pas, mais dans la documentation du Logiciel et dans le Devis accepté. Chaque
Agent indique en outre, dans sa console et par la commande
`senhub-agent license status`, les types de sondes qui requièrent une licence et
ceux que la sienne autorise.

Cette indication est fournie à titre informatif et pour la commodité du Client. Une
erreur d'affichage ne crée aucun droit d'usage : en cas de divergence, le Devis
accepté et la documentation prévalent.

5.3 — Le passage d'une Sonde Libre au régime sous licence pendant la durée du
contrat ne peut être opposé au Client : une sonde qu'il utilisait sans licence le
reste jusqu'au terme de la période en cours. La reconduction prévue à l'article 7.2
ouvre une nouvelle période, au régime en vigueur à sa date de prise d'effet.

## Article 6 — Mise en œuvre technique du contrôle

6.1 — Sensor Factory expose ici le fonctionnement du contrôle de licence **dans la
version du Logiciel livrée à la date de souscription**, afin que le Client puisse
l'auditer et en tenir compte dans sa propre analyse de risque. Cet exposé décrit un
état du Logiciel ; il ne fige pas son évolution, que l'article 6.8 encadre.

6.2 — **Une vérification locale.** Le Jeton de Licence est un jeton signé par une
clé privée détenue par Sensor Factory. L'Agent vérifie cette signature avec la clé
publique correspondante, incluse dans le binaire.

6.3 — **Aucune connexion sortante.** La vérification n'émet aucune requête réseau.
L'Agent ne contacte aucun serveur de licence, à l'installation comme à l'exécution.
Un Agent coupé de tout réseau public fonctionne à l'identique.

6.4 — **Aucune remontée d'usage.** Sensor Factory ne reçoit, du fait du contrôle de
licence, aucune donnée relative au Parc du Client : ni le nombre d'Agents, ni leur
identité, ni les sondes activées, ni les machines supervisées.

6.5 — **Une période de tolérance.** À l'échéance du Jeton de Licence, les Sondes
sous Licence continuent de fonctionner pendant sept (7) jours. Cette tolérance est
une facilité technique destinée à couvrir le délai matériel de renouvellement. Elle
**ne proroge pas le droit d'usage**, dont l'échéance demeure celle que porte le
Jeton de Licence.

6.6 — **Une dégradation partielle.** Passée cette période, les seules Sondes sous
Licence cessent de collecter. L'Agent continue de fonctionner, les Sondes Libres
continuent de collecter, et les sorties configurées continuent d'émettre.

6.7 — Le Jeton de Licence porte un titulaire. Selon ce que les Parties conviennent,
il désigne le Client, et vaut alors pour l'ensemble de son Parc, ou un Agent
déterminé par sa Clé d'Agent, et ne vaut alors que pour lui. La première forme est
celle retenue par défaut ; sous cette forme, un même jeton active tous les Agents du
Parc sans déclaration ni activation individuelle.

6.8 — **Évolution du mécanisme, et garantie d'une voie hors ligne.** **Sensor
Factory s'engage à ce que tout mécanisme de contrôle de licence, dans les versions
publiées pendant la durée du contrat, comporte une voie hors ligne** : une voie
permettant au Client d'obtenir et d'installer l'autorisation requise sans aucun flux
sortant depuis le Parc, par un échange de fichiers hors bande. Le Client demeure
entièrement libre de la politique de sécurité de son système d'information, et
notamment de fermer tout flux sortant ; il lui appartient alors d'employer cette
voie. La fermeture de flux réseau au titre de cette politique ne constitue pas, en
elle-même, un contournement au sens de l'article 4.5.

Sous cette garantie, Sensor Factory peut faire évoluer le contrôle de licence dans
les versions ultérieures du Logiciel, notamment pour y introduire une déclaration,
une activation ou une vérification à distance, le cas échéant Agent par Agent. Un tel
mécanisme est limité aux données nécessaires au contrôle de la licence et à la
facturation ; il ne porte en aucun cas sur les données que le Logiciel collecte pour
le compte du Client. Les évolutions de cette nature sont annoncées dans les notes de
la version qui les introduit. Le Client demeure libre de ne pas déployer une version,
sous réserve de l'article 8.4 ; lorsqu'il en déploie une, il se soumet au contrôle
par l'une des voies que le Logiciel propose.

À défaut de voie hors ligne dans une version, le Client n'est pas tenu de la
déployer et conserve le bénéfice de l'article 8.4 sur la version précédente. Si
aucune version supportée ne comporte de voie hors ligne, il peut résilier le Contrat
de Licence et obtenir le remboursement de la fraction du prix correspondant à la
période restant à courir. **Ce remède épuise les obligations de Sensor Factory au
titre du présent alinéa.**

## Article 7 — Durée et renouvellement

7.1 — Le contrat prend effet à la remise du Jeton de Licence et court jusqu'à
l'échéance que ce jeton porte.

7.2 — **Reconduction tacite.** Il se renouvelle ensuite par tacite reconduction,
par périodes successives de douze (12) mois, matérialisées par la remise d'un
nouveau Jeton de Licence. Chaque Partie peut s'y opposer en informant l'autre par
écrit au plus tard **trois (3) mois** avant l'échéance de la période en cours, sans
avoir à motiver sa décision et sans indemnité de part ni d'autre. Le délai de
préavis du présent article se substitue à celui de l'article 22.5 des CGV.

7.3 — **Information et conditions financières.** Sensor Factory informe le Client de
l'échéance au moins **quatre (4) mois** avant celle-ci, et lui communique à cette
occasion les conditions financières de la période suivante. À défaut de
communication de nouvelles conditions, celles de la période échue sont reconduites.
**À défaut d'information dans ce délai, le Client peut s'opposer à la reconduction
jusqu'au trentième (30e) jour suivant la réception de cette information**, le cas
échéant après la prise d'effet de la nouvelle période, qui prend alors fin à cette
date moyennant remboursement au prorata. La non-reconduction produit les effets
décrits à l'article 6, sans autre formalité.

7.4 — **Coût des composants et infrastructures tiers.** Les conditions financières
sont établies au regard des composants logiciels et des services d'infrastructure
tiers que le Logiciel met en œuvre à la date de la commande, et de leur régime de
mise à disposition à cette même date. Si l'un de ces composants ou services devient
payant, si ses conditions tarifaires évoluent, ou si une fonctionnalité du Logiciel
vient à dépendre d'un composant ou d'une infrastructure donnant lieu à redevance,
Sensor Factory peut réviser ses conditions financières à concurrence du coût
supplémentaire qu'elle supporte. La révision prend effet au renouvellement suivant,
est notifiée avec l'information prévue à l'article 7.3 et motivée à la demande du
Client.

Cette révision est **distincte de l'indexation annuelle prévue par les CGV** et ne
s'impute pas sur le plafond qu'elles fixent. Elle ne peut porter que sur le coût
supplémentaire effectivement supporté par Sensor Factory et n'est mise en œuvre que
lorsque ce coût excède cinq pour cent (5 %) du prix de la période échue. Le Client
conserve en toute hypothèse la faculté de s'opposer à la reconduction dans les
conditions de l'article 7.2.

7.5 — **Retenues à la source.** Les prix s'entendent nets de toute retenue. Si une
réglementation étrangère impose au Client de pratiquer une retenue à la source sur
les sommes dues à Sensor Factory, notamment au titre des redevances, **le Client
majore son paiement du montant nécessaire** pour que Sensor Factory perçoive la
somme qu'elle aurait perçue en l'absence de retenue. Le Client lui remet sans délai
les justificatifs permettant d'imputer cette retenue, et les Parties font
application, le cas échéant, de la convention fiscale applicable.

## Article 8 — Versions et maintenance

8.1 — Pendant la durée du contrat, le Client accède aux versions correctives et
évolutives du Logiciel publiées par Sensor Factory **pour le niveau qu'il a
souscrit**, sans supplément de prix. Il en décide seul le déploiement.

Sensor Factory demeure libre de commercialiser séparément une fonctionnalité
nouvelle, sous la forme d'un niveau supérieur ou d'un module distinct. Une telle
fonctionnalité n'est pas comprise dans la présente concession, ce que les notes de
la version qui l'introduit indiquent.

8.2 — Sensor Factory ne garantit pas la compatibilité ascendante des formats de
sortie entre versions majeures. Les changements de cette nature sont annoncés dans
les notes de version, avec le chemin de migration.

8.3 — Le support technique, lorsqu'il est souscrit, relève d'un contrat distinct et
n'est pas compris dans la présente concession.

8.4 — **Versions supportées.** Sensor Factory n'est tenue de corriger que la version
courante et la version majeure qui la précède. Les engagements des articles 8.1 et 11
ne jouent qu'au bénéfice d'un Client exploitant une version supportée.

8.5 — **Nature de l'obligation.** Le Logiciel est un outil de collecte et de
restitution de données techniques. Il n'analyse pas les données qu'il restitue, ne
définit aucun seuil et n'émet par lui-même aucun jugement sur l'état des systèmes
supervisés. **Le Client définit seul** les systèmes à superviser, les sondes
activées, leur fréquence, les destinations de sortie et, dans les outils dont il
dispose, les seuils et les règles d'alerte.

Sensor Factory s'oblige à mettre à disposition un Logiciel **conforme à sa
documentation** pour la version considérée, et à corriger, dans les conditions de
l'article 8.4, les défauts de conformité qui lui sont signalés. Elle est tenue à cet
égard d'une **obligation de moyens**, conformément à l'article 10.1 des CGV.

Elle ne garantit pas un fonctionnement ininterrompu ou exempt d'erreur, ni
l'exactitude ou l'exhaustivité des données que les systèmes supervisés lui
renvoient. La détection d'un incident et l'émission d'une alerte procédant de la
configuration, des seuils et des outils que le Client retient, **elles ne relèvent
pas des obligations de Sensor Factory**.

Le Logiciel ne constitue ni un dispositif de sécurité des personnes ou des biens,
ni un système de conduite de processus, ni un dispositif de continuité d'activité.
Il ne se substitue ni aux procédures d'exploitation du Client, ni à sa supervision
humaine, ni à ses propres dispositifs de sauvegarde et de reprise. Le Client
demeure seul responsable de la configuration qu'il retient et des décisions qu'il
prend au vu des données restituées.

8.6 — **Cœur ouvert.** Les composants publiés sous licence Apache 2.0 sont fournis
selon les termes de cette licence, **en l'état et sans garantie**. Le présent contrat
n'ajoute aucune garantie à leur égard.

8.7 — **Versions d'évaluation.** Sensor Factory peut mettre à disposition des
versions préliminaires, identifiées comme telles par leur numéro ou par les notes de
version. Elles sont fournies **en l'état et sans garantie**, pour évaluation et hors
production. Les articles 8.1, 8.4 et 11 ne leur sont pas applicables, et elles ne
constituent pas des versions supportées au sens de l'article 8.4.

## Article 9 — Propriété intellectuelle

9.1 — Sensor Factory demeure titulaire de l'ensemble des droits de propriété
intellectuelle sur les composants sous licence. Le présent Contrat de Licence
n'emporte aucune cession.

9.2 — Les marques des éditeurs et constructeurs tiers, citées dans le Logiciel et
dans sa documentation pour désigner les systèmes supervisés, demeurent la propriété
de leurs titulaires respectifs. Leur mention n'emporte ni partenariat ni
certification.

## Article 10 — Composants tiers

10.1 — Le Logiciel incorpore des composants développés par des tiers et distribués
sous leurs propres licences, pour l'essentiel des licences libres. Ces licences
s'appliquent à ces composants et prévalent, **pour eux seuls**, sur le présent
Contrat de Licence.

10.2 — Aucune stipulation du présent Contrat de Licence ne restreint les droits que
le Client tient de ces licences, ni ne lui en impose au-delà de ce qu'elles
prévoient.

10.3 — La liste de ces composants et de leurs licences fait l'objet de
l'**Annexe 1**. Cette annexe se rapporte à une version déterminée du Logiciel et
est mise à jour à chaque version publiée. Sensor Factory remet au Client, sur
simple demande, l'annexe correspondant à la version qu'il exploite ; elle
accompagne également le Logiciel et figure dans sa documentation.

10.4 — Sensor Factory ne concède aucun droit sur ces composants au-delà de ce que
leurs titulaires concèdent, et n'assume à leur égard aucune obligation de garantie
ni de maintien en condition.

## Article 11 — Garantie d'éviction et prise en charge des actions en contrefaçon

11.1 — Sensor Factory déclare détenir les droits nécessaires à la concession
consentie par le présent Contrat de Licence.

11.2 — Si un tiers engage contre le Client une action fondée sur le fait que les
composants sous licence, dans une version supportée au sens de l'article 8.4 et
employés conformément au présent Contrat de Licence, porteraient atteinte à ses
droits de propriété intellectuelle, Sensor Factory prend l'action en charge et
supporte les sommes mises à la charge du Client **par une décision de justice
passée en force de chose jugée**, ou par une transaction qu'elle a elle-même
conclue ou approuvée par écrit.

Ne sont pas couverts les coûts internes du Client, les conseils qu'il engagerait de
son propre chef, ni aucun préjudice indirect au sens des CGV.

11.3 — Cette garantie est subordonnée à quatre conditions cumulatives : que le
Client avise Sensor Factory **sans délai et par écrit**, et en tout état de cause
dans un délai qui ne compromet pas la défense ; qu'il lui laisse la direction de
l'action et toute liberté pour transiger ; qu'il lui apporte les informations et
l'assistance nécessaires à sa défense ; qu'il s'abstienne de toute reconnaissance de
responsabilité et de toute transaction sans son accord écrit.

11.4 — Si l'usage du Logiciel venait à être interdit, Sensor Factory, **à son choix
et à ses frais**, obtient le droit de poursuivre l'usage, remplace ou modifie
l'élément en cause de manière à faire cesser l'atteinte, ou rembourse au Client la
fraction du prix correspondant à la période restant à courir. Ce remboursement met
fin au Contrat de Licence pour l'avenir et épuise les obligations de Sensor Factory
au titre du présent article.

11.5 — La garantie ne joue pas lorsque l'atteinte résulte d'une modification du
Logiciel par le Client ou par un tiers, d'une combinaison avec des éléments que
Sensor Factory n'a pas fournis, d'un usage non conforme au présent Contrat de
Licence, du maintien d'une version non supportée au sens de l'article 8.4, du
maintien de l'usage de l'élément en cause après que Sensor Factory a mis à
disposition une version corrigée ou de remplacement ou invité le Client par écrit à
en cesser l'usage, ou d'un composant tiers au sens de l'article 10.

11.6 — Les stipulations du présent article constituent la seule garantie de Sensor
Factory au titre des droits de propriété intellectuelle **des tiers** et, sous cette
réserve, **épuisent les recours** du Client à ce titre. Elles s'exercent dans la limite du plafond de responsabilité
fixé par les CGV et, si ce plafond venait à ne pas trouver à s'appliquer, dans la
limite des sommes effectivement perçues par Sensor Factory au titre du présent
Contrat de Licence au cours des douze (12) mois précédant le fait générateur.

Le présent article **ne limite ni n'écarte** la garantie due par Sensor Factory à
raison d'un fait qui lui est personnel, ni sa responsabilité en cas de **dol ou de
faute lourde**.

## Article 12 — Données

12.1 — Le Logiciel collecte des données techniques sur les systèmes que le Client
lui désigne et les transmet aux destinations que le Client configure. Sensor Factory
n'est destinataire d'aucune de ces données, sauf si le Client configure expressément
une sortie vers une plateforme exploitée par Sensor Factory, auquel cas les
conditions de cette plateforme s'appliquent en complément.

12.2 — Le Client demeure **responsable de traitement** au sens du règlement
2016/679 pour les données que l'Agent collecte. Il lui appartient de vérifier que
les sondes qu'il active ne collectent pas de données à caractère personnel au-delà
de ce que sa propre analyse autorise.

Sensor Factory **ne reçoit aucune de ces données**, ne les traite pour aucune
finalité propre et n'en détermine ni les finalités ni les moyens. Elle n'intervient
donc à leur égard **ni comme responsable de traitement, ni comme sous-traitant** au
sens de l'article 28 du même règlement. L'article 12.2 des CGV ne s'applique pas à
ces données.

Le Client **s'engage à ne pas configurer l'Agent de manière à transmettre à Sensor
Factory des données à caractère personnel**. S'il envisage une configuration qui
aurait cet effet, il en informe Sensor Factory par écrit et préalablement, et les
Parties conviennent des stipulations requises par l'article 28 du règlement 2016/679
avant sa mise en œuvre.

**Par exception**, si une version du Logiciel met en œuvre un mécanisme au sens de
l'article 6.8, Sensor Factory est **responsable de traitement** pour les seules
données que ce mécanisme lui transmet aux fins du contrôle de la licence et de la
facturation. Elle en informe le Client dans les notes de la version et dans sa
politique de confidentialité.

12.3 — **Garantie du Client.** Le Client garantit Sensor Factory contre toute action
d'un tiers ou d'une personne concernée fondée sur les données que l'Agent collecte,
transporte ou restitue sur sa configuration, ou sur les systèmes qu'il a désignés à
l'Agent, et en supporte les conséquences pécuniaires.

Si une autorité de contrôle engage à l'encontre de Sensor Factory une procédure
ayant pour objet ces mêmes données, le Client lui apporte sans délai les
informations et l'assistance nécessaires à sa défense et **prend en charge les frais
de défense exposés**. Les sanctions pécuniaires prononcées par une autorité
demeurent à la charge de la Partie à laquelle elles sont infligées.

Cette garantie s'exerce dans les conditions de l'article 11.3, appliquées au
bénéfice de Sensor Factory.

## Article 13 — Vérification de conformité

13.1 — Tant que le contrôle décrit à l'article 6 ne mesure pas l'usage, la
conformité repose d'abord sur la déclaration du Client prévue à l'article 4.4. Le Client est
tenu de la sincérité de cette déclaration.

13.2 — Sensor Factory peut, **une fois par période de douze (12) mois** et moyennant
un préavis de **trente (30) jours**, demander au Client une attestation écrite du
niveau souscrit et du périmètre d'usage, signée par une personne habilitée.

13.3 — **Vérification.** En cas d'indice sérieux d'un usage excédant le périmètre
souscrit, Sensor Factory peut faire procéder à une vérification, par elle-même ou
par un tiers indépendant tenu au secret, moyennant un préavis de **trente (30)
jours**. Cette vérification se déroule pendant les heures ouvrées, sans perturber
l'exploitation, et ne porte que sur les éléments nécessaires à établir le périmètre.
Elle n'ouvre aucun accès aux données que le Logiciel collecte pour le compte du
Client.

La vérification s'opère à partir des inventaires et des journaux dont le Client
dispose, selon une méthode que Sensor Factory lui communique avec le préavis. **Elle
ne requiert l'exécution d'aucun programme fourni par Sensor Factory dans le système
d'information du Client.** Ses conclusions sont établies contradictoirement.

13.4 — **Frais et régularisation.** Les frais de vérification sont supportés par
Sensor Factory. Ils sont à la charge du Client lorsque la vérification établit un
usage excédant le Périmètre Souscrit au-delà de la tolérance prévue à l'article 4.3.

Dans ce cas, **ou lorsque le Client a omis l'information prévue à l'article 4.3**,
le Client règle la différence de prix pour la période au cours de laquelle le
dépassement est établi, et au plus pour les **douze (12) mois** précédant la
notification de la vérification ou de l'omission. La régularisation est calculée aux
conditions financières du Devis accepté.

Elle est calculée au tarif public en vigueur, **majorée de vingt pour cent (20 %)**,
lorsque le dépassement procède d'une **déclaration sciemment inexacte** ou de la
mise en échec des mécanismes de l'article 6.

Le règlement de cette régularisation vaut extension du Périmètre Souscrit pour la
période en cours. **Sensor Factory ne peut soumettre à une nouvelle vérification les
usages ainsi régularisés.**

13.5 — Le manquement délibéré aux articles 4.4, 13.2 ou 13.3 constitue un manquement grave
au sens de l'article 16.

## Article 14 — Conformité : exportations et probité

14.1 — Le Client s'engage à respecter les réglementations françaises, européennes
et, le cas échéant, étrangères applicables au contrôle des exportations et aux
mesures restrictives.

14.2 — Il s'interdit en particulier d'exporter, de réexporter, de céder ou de mettre
à disposition le Logiciel, directement ou indirectement, au bénéfice d'un pays,
d'une personne ou d'une entité visés par une mesure d'embargo ou de gel des avoirs,
notamment au titre des règlements de l'Union européenne, de l'*Export Administration
Regulations* et des programmes de sanctions administrés par l'*Office of Foreign
Assets Control* des États-Unis,
ainsi que de l'employer à une fin prohibée, notamment en rapport avec des armes
nucléaires, chimiques ou biologiques ou leurs vecteurs.

14.3 — Le Client déclare n'être lui-même visé par aucune de ces mesures et informe
Sensor Factory sans délai s'il venait à l'être. Il garantit Sensor Factory contre les conséquences
d'un manquement au présent article, qui constitue en outre un manquement grave au
sens de l'article 16. Les sanctions pécuniaires prononcées par une autorité
demeurent à la charge de la Partie à laquelle elles sont infligées.

14.4 — **Probité.** Chaque Partie déclare respecter les réglementations applicables
en matière de lutte contre la corruption et le trafic d'influence, notamment la loi
n° 2016-1691 du 9 décembre 2016, le *UK Bribery Act 2010* et le *Foreign Corrupt
Practices Act*, et s'interdit d'offrir, de promettre ou d'accorder un paiement ou un
avantage quelconque en vue d'obtenir un avantage indu. Elle s'engage à ce que ses
dirigeants, préposés et sous-traitants intervenant au titre du contrat respectent
les mêmes règles. Le manquement au présent article constitue un manquement grave au
sens de l'article 16.

## Article 15 — Cession et changement de contrôle

15.1 — Le Client ne peut céder le présent Contrat de Licence, ni les droits et
obligations qui en découlent, sans l'accord écrit préalable de Sensor Factory. En
cas de transmission universelle de son patrimoine, le contrat se poursuit de plein
droit avec le bénéficiaire, moyennant information préalable de Sensor Factory.

15.2 — Sensor Factory peut céder le présent Contrat de Licence à une société de son
groupe ou à l'acquéreur de la branche d'activité concernée, moyennant information du
Client.

15.3 — **Une précision s'impose, la concession portant sur le Parc entier.** En cas
de prise de contrôle du Client, le Parc couvert demeure celui qui existait à la date
de l'opération, augmenté de sa croissance propre. L'extension de la concession au
parc de l'acquéreur ou des entités qu'il contrôle par ailleurs fait l'objet d'un
avenant.

15.4 — Il en va de même dans l'autre sens. Lorsque le Client prend le contrôle d'une
entité pendant la durée du contrat, le parc de cette entité n'entre dans le Parc
qu'au renouvellement suivant, sur la base de la déclaration prévue à l'article 4.4
et des conditions financières qui en découlent.

## Article 16 — Résiliation

16.1 — Chaque Partie peut résilier le contrat en cas de manquement grave de l'autre,
non réparé dans les **trente (30) jours** de sa mise en demeure.

16.2 — **Suspension.** En cas de défaut de paiement à l'échéance, ou de manquement
aux articles 4.5, 4.6, 13 ou 14, Sensor Factory peut suspendre la délivrance de tout
nouveau Jeton de Licence, sans formalité autre qu'une notification écrite et sans
préjudice de son droit à résiliation.

16.3 — La résiliation emporte **cessation immédiate du droit d'usage des Sondes sous
Licence**. Le Client cesse cet usage et en atteste par écrit dans les trente (30)
jours. Il n'est pas tenu de désinstaller l'Agent ni privé des Sondes Libres, dont
l'usage relève de la licence Apache 2.0.

16.4 — Les sommes versées au titre de la période en cours restent acquises à Sensor
Factory. En cas de résiliation à ses torts, elle rembourse la fraction du prix
correspondant à la période restant à courir. **Ce remboursement s'impute sur toute
indemnité due, dans les limites fixées par les CGV et rappelées à l'article 17.2.** Les sommes restant dues deviennent immédiatement exigibles.

16.5 — **Survie.** Survivent à la fin du contrat, quelle qu'en soit la cause, les
articles 1.4, 2.4, 4.5 à 4.7, 8.5 à 8.7, 9 à 14 et 17, ainsi que toute stipulation dont la nature
commande qu'elle demeure.

## Article 17 — Droit applicable, responsabilité et langue

17.1 — Le présent Contrat de Licence est soumis au **droit français**. La
Convention des Nations unies sur les contrats de vente internationale de
marchandises du 11 avril 1980 ne lui est pas applicable.

17.2 — **Limitations de responsabilité.** Les stipulations des CGV relatives à la
nature de l'obligation de Sensor Factory, à l'exclusion du préjudice indirect, au
plafond de responsabilité, au délai de réclamation, à la force majeure et au
règlement des litiges s'appliquent au présent Contrat de Licence. Elles couvrent
l'intégralité des obligations qu'il stipule, y compris les engagements, déclarations
et garanties qu'il contient, **ainsi que les obligations de prise en charge et
d'indemnisation des articles 11 et 12**.

Conformément à l'article 1.4, elles prévalent sur toute stipulation contraire de
tout autre document contractuel ; toute stipulation qui y dérogerait sans la
renonciation expresse prévue à l'article 1.4 est **sans effet**.

Si l'une de ces stipulations venait à ne pas trouver à s'appliquer, la
responsabilité de Sensor Factory demeure limitée aux sommes qu'elle a effectivement
perçues au titre du présent Contrat de Licence au cours des **douze (12) mois**
précédant le fait générateur.

Les limitations et exclusions prévues au présent article ne s'appliquent ni au
**dol**, ni à la **faute lourde**, ni aux **dommages corporels**, ni à toute
responsabilité qui ne peut être limitée en vertu de la loi applicable.

Les Parties reconnaissent que les conditions financières du présent Contrat de
Licence reflètent la répartition des risques qui résulte du présent article, et que
**Sensor Factory n'aurait pas contracté sans celle-ci**.

17.3 — **Force majeure.** L'article 17 des CGV s'applique au présent contrat. Sont
notamment susceptibles de constituer un cas de force majeure, **lorsqu'ils
réunissent les conditions de l'article 1218 du code civil** : les catastrophes
naturelles, les incendies et inondations, les guerres, actes de terrorisme et
troubles civils, les épidémies et les mesures sanitaires qui s'y rapportent, les
décisions d'une autorité publique, les grèves générales ou sectorielles, les
cyberattaques d'ampleur, et l'interruption prolongée des réseaux publics de
télécommunications ou de fourniture d'énergie.

17.4 — Le présent Contrat de Licence est rédigé en **langue française**. Sensor
Factory peut en remettre une traduction, notamment en langue anglaise, pour la
commodité du Client. Cette traduction n'a qu'une valeur d'information : **seule la
version française fait foi** entre les Parties, et elle seule sera opposée devant
toute juridiction. En cas de divergence entre la version française et une
traduction, quelle qu'en soit la cause, la version française prévaut.

---

## Signatures

Fait à ……………………………, le …………………………, en deux exemplaires originaux.

**Pour SENSOR FACTORY SAS** — Nom : ………………………… Qualité : …………………………

**Pour le Client** — Dénomination : ………………………… Nom : ………………………… Qualité :
…………………………

Le présent contrat peut être signé par voie électronique, dans les conditions des
articles 1366 et 1367 du code civil et du règlement (UE) n° 910/2014. Il peut être
signé en plusieurs exemplaires séparés, l'ensemble formant un seul et même acte.

---

## Annexe 1 — Composants tiers

Cette annexe se rapporte à une version déterminée du Logiciel, indiquée en tête
de tableau. Elle est produite depuis le graphe de dépendances de la
construction, et décrit donc ce qui est effectivement lié dans le binaire
distribué, et non ce que le fichier de dépendances déclare : un composant
utilisé par les seuls tests n'y figure pas.

Les licences ci-dessous s'appliquent à ces composants seuls et prévalent, pour
eux, sur le présent Contrat de Licence, conformément à l'article 10.

La liste des composants tiers liés dans le binaire distribué, avec leurs
licences respectives, est publiée et tenue à jour à la page
[Composants tiers](third-party.md). Elle est produite depuis le graphe de
dépendances de la construction, et décrit donc ce qui est effectivement lié
dans le binaire distribué.
