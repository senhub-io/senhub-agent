# SenHub Agent License Agreement

*Version 1.0 — supplement to the General Terms and Conditions of Sale of
Sensor Factory. Applicable to the SenHub Agent software, all versions.*

!!! warning "Courtesy translation — not the binding text"
    This English text is provided for convenience only. The agreement is
    drawn up in French and **only the French version is binding** between
    the parties; see [Contrat de licence SenHub Agent](agreement-fr.md),
    also available as a [PDF](contrat-de-licence-senhub-agent.pdf). Where
    the two differ, for any reason, the French version prevails.

    Certain notions of French law used here have no exact equivalent in
    common law. They are reproduced in French in brackets so that the
    intended meaning is not lost in translation.

---

## Article 1 — Purpose and standing of this agreement

1.1 — This agreement (the "**License Agreement**") governs the grant, by
**SENSOR FACTORY SAS** (hereinafter "Sensor Factory"), of the right to use the
licensed components of the **SenHub Agent** software (the "**Software**").

1.2 — It supplements the General Terms and Conditions of Sale of Sensor Factory
in force at the date of the order (the "**GTC**"), which remain the foundation of
the commercial relationship and govern everything this agreement does not
expressly address.

1.3 — **Formation.** Acceptance of the Order Form (*Devis*) by the Customer, by
signature or by the issue of a purchase order referring to it, constitutes
acceptance of this License Agreement and of the GTC. The Order Form states the
subscribed level and the sizing parameters. The parties may agree Special
Conditions (*Conditions particulières*); either party may request that such
conditions be drawn up before the Order Form is accepted.

1.4 — **Order of precedence.** For what each governs, the contractual documents
apply in the following order: (1) the Special Conditions, where the parties sign
any; (2) this License Agreement, for the grant of the right to use the Software,
its scope, its term and its control; (3) the accepted Order Form, for the
subscribed level, the sizing and the price; (4) the GTC. In the event of
conflict, the higher-ranking document prevails.

**By way of exception**, the provisions of the GTC referred to in Article 17.2
prevail over any other document, whatever its rank, including the Special
Conditions and the Order Form, save for an **express** waiver by Sensor Factory,
in writing, naming the provision derogated from and signed by a corporate officer.

## Article 2 — Structure of the Software

2.1 — The Software consists of two sets of components under different legal
regimes. This distinction governs the reading of everything that follows.

2.2 — **The open core.** Part of the Software, described in its documentation, is
published under the **Apache 2.0** license and its source code is public. The
Customer holds it on the terms of that license, independently of this License
Agreement.

That license is acquired, for each version, in the version in which the component
was published: what has been published under Apache 2.0 remains so. Sensor
Factory does not, however, undertake any particular division between the open
core and the licensed components in future versions, and remains free to publish
a new component under either regime.

2.3 — **The licensed probes.** Certain probe types, addressing equipment and
software from third-party vendors, are not covered by the Apache 2.0 license.
Their use is granted by this License Agreement, for consideration, for the term
it sets.

2.4 — One consequence follows, which the Customer should know before signing:
the expiry or termination of this License Agreement deprives the Customer of none
of the rights it holds under the Apache 2.0 license over the open core. It bears
on the Licensed Probes alone. The corresponding behaviour of the Software is
described in Article 6.6.

## Article 3 — Definitions

Terms defined by the GTC keep the same meaning here. In addition:

- **Agent**: an instance of the Software installed on a machine, physical or
  virtual, or run in a container.
- **Estate** (*Parc*): all Agents operated by the Customer and by the entities it
  controls within the meaning of Article L.233-3 of the French Commercial Code,
  subject to Article 15.4. The Customer answers for those entities' compliance
  with this agreement **as for its own**, and undertakes (*se porte fort*) that
  they will cooperate with the verification provided for in Article 13.3.
- **Subscribed Scope** (*Périmètre Souscrit*): the sizing parameters stated in the
  accepted Order Form, in particular the number of Agents, within the limits of
  which the right of use is granted. Failing a figure in the Order Form, the
  Subscribed Scope is that which the Customer declared when ordering and, failing
  any declaration, the number of Agents in service in the Estate at the date the
  agreement takes effect.
- **Agent Key**: the identifier each Agent produces itself on first run. It is
  never entered by the Customer nor assigned by Sensor Factory.
- **License Token**: the signed file Sensor Factory delivers to the Customer,
  carrying the granted level, the scope of authorised probes, the holder and the
  expiry date.
- **Licensed Probe**: a probe type whose use requires a valid License Token.
- **Free Probe**: any other probe type, usable without a License Token and without
  financial consideration.

## Article 4 — Grant

4.1 — Sensor Factory grants the Customer, for the term set in Article 7 and
subject to payment of the agreed price, a right to use the Licensed Probes
corresponding to the subscribed level.

4.2 — That right is **non-exclusive**, **non-transferable** and **not
sublicensable**. It is granted for the Customer's own needs, to the exclusion of
any operation on behalf of third parties, whether for consideration or not.

4.3 — **Scope.** The right is exercised over the Estate within the limits of the
Subscribed Scope.

The means by which that right is implemented on each Agent, whether by
declaration, activation or verification, are those of the version of the Software
the Customer deploys. Article 6 sets them out as they stand at the date of
subscription and Article 6.8 governs their evolution. None of these means alters
the Subscribed Scope, whether or not it makes it easier to monitor.

**Exceeding** any quantitative element of the Subscribed Scope, taken separately,
gives rise to no adjustment within the limit of **five per cent (5%)** of the
subscribed value for that element, rounded up to the next whole unit. Beyond
that, the Customer informs Sensor Factory within thirty (30) days and the parties
agree the corresponding extension, invoiced at the list price in force and pro
rata for the remainder of the period. Failing such information within that
period, Article 13.4 applies.

4.4 — **At each renewal**, the Customer declares to Sensor Factory in writing the
elements constituting the Subscribed Scope for the following period. That
declaration is a condition of renewal and forms the basis of the price. An
inaccurate declaration produces the effects set out in Article 13.

4.5 — **The Customer shall not** circumvent, neutralise or remove the mechanisms
described in Article 6, disclose its License Token to a third party, offer the
Licensed Probes within a service offering to third parties, or decompile the
licensed components outside the cases where the law mandatorily permits it.
Article L.122-6-1 of the French Intellectual Property Code is reserved.

4.6 — The Customer shall likewise not remove or alter the ownership, license and
version notices carried by the Software.

4.7 — The Customer shall not publish the results of comparative benchmarks
concerning the Licensed Probes without the prior written consent of Sensor
Factory. Such consent shall not be withheld without reason. Sensor Factory has
thirty (30) days to respond; **its silence on expiry of that period constitutes
consent**.

## Article 5 — Levels and scope

5.1 — There are three levels. The subscribed level appears in the accepted Order
Form and is reflected in the License Token.

- **Free**: all Free Probes. No License Token, no financial consideration, no
  limit of term or of number of Agents. This level requires no subscription; this
  agreement governs it only where a paid level is otherwise subscribed.
- **Pro**: the Free Probes, and the Licensed Probes which the product
  documentation designates as such at the date of subscription.
- **Enterprise**: the Free Probes, the Licensed Probes, and those Sensor Factory
  may publish during the term of the agreement, excluding modules which Sensor
  Factory designates, upon their publication, as being separately priced, in
  particular where they depend on a third-party component or infrastructure giving
  rise to a fee.

5.2 — The granted scope is not to be read in this License Agreement, which does
not fix it, but in the documentation of the Software and in the accepted Order
Form. Each Agent further indicates, in its console and through the
`senhub-agent license show` command, which probe types require a license and
which ones its own license authorises.

That indication is provided for information and for the Customer's convenience. A
display error creates no right of use: in the event of divergence, the accepted
Order Form and the documentation prevail.

5.3 — The move of a Free Probe to the licensed regime during the term of the
agreement may not be invoked against the Customer: a probe it was using without a
license remains so until the end of the current period. The renewal provided for
in Article 7.2 opens a new period, under the regime in force at its effective date.

## Article 6 — How the license control is implemented

6.1 — Sensor Factory sets out here how the license control works **in the version
of the Software delivered at the date of subscription**, so that the Customer may
audit it and take it into account in its own risk analysis. This description
records a state of the Software; it does not fix its evolution, which Article 6.8
governs.

6.2 — **A local check.** The License Token is a token signed with a private key
held by Sensor Factory. The Agent verifies that signature with the corresponding
public key, embedded in the binary.

6.3 — **No outbound connection.** The verification issues no network request. The
Agent contacts no license server, at installation or at run time. An Agent cut off
from every public network behaves identically.

6.4 — **No usage reporting.** Sensor Factory receives, by reason of the license
control, no data concerning the Customer's Estate: neither the number of Agents,
nor their identity, nor the probes enabled, nor the machines monitored.

6.5 — **A grace period.** On expiry of the License Token, the Licensed Probes
continue to operate for seven (7) days. That tolerance is a technical convenience
intended to cover the practical delay of renewal. It **does not extend the right
of use**, whose expiry remains that carried by the License Token.

6.6 — **Partial degradation.** After that period, only the Licensed Probes stop
collecting. The Agent keeps running, the Free Probes keep collecting, and the
configured outputs keep sending.

6.7 — The License Token carries a holder. According to what the parties agree, it
designates the Customer, and is then valid for its whole Estate, or a particular
Agent identified by its Agent Key, and is then valid for that Agent alone. The
first form is the default; in that form, a single token activates every Agent in
the Estate with no individual declaration or activation.

6.8 — **Evolution of the mechanism, and guarantee of an offline path.** **Sensor
Factory undertakes that any license-control mechanism, in the versions published
during the term of the agreement, shall include an offline path**: a path
allowing the Customer to obtain and install the required authorisation with no
outbound flow whatsoever from the Estate, by an out-of-band exchange of files.
The Customer remains entirely free in the security policy of its information
system, and in particular free to close all outbound flows; it is then for the
Customer to use that path. Closing network flows under such a policy does not, in
itself, constitute circumvention within the meaning of Article 4.5.

Subject to that guarantee, Sensor Factory may develop the license control in later
versions of the Software, in particular to introduce a declaration, an activation
or a remote verification, where applicable Agent by Agent. Any such mechanism is
limited to the data necessary for license control and for invoicing; it bears in
no case on the data the Software collects on the Customer's behalf. Changes of
this nature are announced in the notes of the version introducing them. The
Customer remains free not to deploy a version, subject to Article 8.4; where it
deploys one, it submits to the control by one of the paths the Software offers.

Failing an offline path in a version, the Customer is not required to deploy it
and retains the benefit of Article 8.4 on the previous version. If no supported
version includes an offline path, the Customer may terminate the License Agreement
and obtain a refund of the portion of the price corresponding to the unexpired
period. **That remedy exhausts Sensor Factory's obligations under this paragraph.**

## Article 7 — Term and renewal

7.1 — The agreement takes effect on delivery of the License Token and runs until
the expiry that token carries.

7.2 — **Automatic renewal.** It then renews automatically, for successive periods
of twelve (12) months, evidenced by the delivery of a new License Token. Either
party may object by informing the other in writing no later than **three (3)
months** before the expiry of the current period, without having to give reasons
and with no indemnity on either side. The notice period under this article
replaces that of Article 22.5 of the GTC.

7.3 — **Information and financial terms.** Sensor Factory informs the Customer of
the expiry at least **four (4) months** beforehand, and communicates on that
occasion the financial terms for the following period. Failing communication of
new terms, those of the expiring period are carried over. **Failing information
within that period, the Customer may object to the renewal until the thirtieth
(30th) day following receipt of that information**, where applicable after the
new period has taken effect, which then ends on that date against a pro rata
refund. Non-renewal produces the effects described in Article 6, with no further
formality.

7.4 — **Cost of third-party components and infrastructure.** The financial terms
are set by reference to the third-party software components and infrastructure
services the Software uses at the date of the order, and to their terms of
availability at that same date. If one of those components or services becomes
chargeable, if its pricing terms change, or if a feature of the Software comes to
depend on a component or infrastructure giving rise to a fee, Sensor Factory may
revise its financial terms to the extent of the additional cost it bears. The
revision takes effect at the following renewal, is notified together with the
information provided for in Article 7.3 and is substantiated at the Customer's
request.

That revision is **distinct from the annual indexation provided for in the GTC**
and is not set against the cap the GTC fixes. It may bear only on the additional
cost actually borne by Sensor Factory and is applied only where that cost exceeds
five per cent (5%) of the price of the expiring period. The Customer retains in
all events the right to object to the renewal on the terms of Article 7.2.

7.5 — **Withholding tax.** Prices are net of any withholding. If a foreign
regulation requires the Customer to apply a withholding tax on sums due to Sensor
Factory, in particular by way of royalties, **the Customer shall gross up its
payment by the necessary amount** so that Sensor Factory receives the sum it would
have received in the absence of withholding. The Customer shall promptly provide
the supporting documents enabling that withholding to be credited, and the parties
shall apply, where relevant, the applicable tax treaty.

## Article 8 — Versions and maintenance

8.1 — During the term of the agreement, the Customer has access to the corrective
and evolutive versions of the Software published by Sensor Factory **for the level
it has subscribed**, at no additional price. It alone decides on their deployment.

Sensor Factory remains free to market a new feature separately, in the form of a
higher level or of a distinct module. Such a feature is not included in this
grant, which the notes of the version introducing it state.

8.2 — Sensor Factory does not warrant backward compatibility of output formats
between major versions. Changes of this nature are announced in the release notes,
together with the migration path.

8.3 — Technical support, where subscribed, falls under a separate agreement and is
not included in this grant.

8.4 — **Supported versions.** Sensor Factory is bound to correct only the current
version and the major version preceding it. The undertakings in Articles 8.1 and
11 operate only for the benefit of a Customer running a supported version.

8.5 — **Nature of the obligation.** The Software is a tool for collecting and
presenting technical data. It does not analyse the data it presents, sets no
threshold and issues of itself no judgement on the state of the monitored systems.
**The Customer alone defines** the systems to be monitored, the probes enabled,
their frequency, the output destinations and, in the tools available to it, the
thresholds and alerting rules.

Sensor Factory undertakes to make available Software **conforming to its
documentation** for the version concerned, and to correct, on the terms of Article
8.4, the non-conformities reported to it. It is bound in that respect by an
obligation of means (*obligation de moyens*) — a duty of reasonable care and skill
and not a guarantee of result — in accordance with Article 10.1 of the GTC.

It does not warrant uninterrupted or error-free operation, nor the accuracy or
completeness of the data the monitored systems return to it. The detection of an
incident and the issuing of an alert arising from the configuration, the
thresholds and the tools the Customer chooses, **they do not fall within Sensor
Factory's obligations**.

The Software is neither a device for the safety of persons or property, nor a
process control system, nor a business continuity device. It replaces neither the
Customer's operating procedures, nor its human supervision, nor its own backup and
recovery arrangements. The Customer remains solely responsible for the
configuration it adopts and for the decisions it takes in the light of the data
presented.

8.6 — **Open core.** The components published under the Apache 2.0 license are
supplied on the terms of that license, **as is and without warranty**. This
agreement adds no warranty in respect of them.

8.7 — **Evaluation versions.** Sensor Factory may make available preliminary
versions, identified as such by their number or by the release notes. They are
supplied **as is and without warranty**, for evaluation and outside production.
Articles 8.1, 8.4 and 11 do not apply to them, and they do not constitute
supported versions within the meaning of Article 8.4.

## Article 9 — Intellectual property

9.1 — Sensor Factory remains the holder of all intellectual property rights in the
licensed components. This License Agreement effects no assignment.

9.2 — The trade marks of third-party vendors and manufacturers, cited in the
Software and in its documentation to designate the monitored systems, remain the
property of their respective holders. Their mention implies neither partnership
nor certification.

## Article 10 — Third-party components

10.1 — The Software incorporates components developed by third parties and
distributed under their own licenses, for the most part open-source licenses.
Those licenses apply to those components and prevail, **for them alone**, over
this License Agreement.

10.2 — No provision of this License Agreement restricts the rights the Customer
holds under those licenses, nor imposes on it any obligation beyond what they
provide.

10.3 — The list of those components and of their licenses is the subject of
**Annex 1**. That annex relates to a specific version of the Software and is
updated with each published version. Sensor Factory provides the Customer, on
simple request, with the annex corresponding to the version it runs; it also
accompanies the Software and appears in its documentation.

10.4 — Sensor Factory grants no right over those components beyond what their
holders grant, and assumes in respect of them no obligation of warranty or of
maintenance.

## Article 11 — Warranty against eviction and conduct of infringement claims

11.1 — Sensor Factory declares that it holds the rights necessary for the grant
made by this License Agreement.

11.2 — If a third party brings against the Customer a claim based on the
allegation that the licensed components, in a version supported within the meaning
of Article 8.4 and used in accordance with this License Agreement, infringe its
intellectual property rights, Sensor Factory shall take conduct of the claim and
bear the sums awarded against the Customer **by a court decision that has become
final and unappealable** (*passée en force de chose jugée*), or under a settlement
it has itself concluded or approved in writing.

Not covered are the Customer's internal costs, advisers it engages of its own
accord, or any indirect loss within the meaning of the GTC.

11.3 — This warranty is subject to four cumulative conditions: that the Customer
notify Sensor Factory **promptly and in writing**, and in any event within a period
that does not compromise the defence; that it leave Sensor Factory conduct of the
claim and full latitude to settle; that it provide the information and assistance
necessary to the defence; and that it refrain from any admission of liability and
from any settlement without Sensor Factory's written consent.

11.4 — Should use of the Software be enjoined, Sensor Factory shall, **at its
option and at its expense**, obtain the right to continue the use, replace or
modify the element concerned so as to end the infringement, or refund the Customer
the portion of the price corresponding to the unexpired period. That refund ends
the License Agreement for the future and exhausts Sensor Factory's obligations
under this article.

11.5 — The warranty does not operate where the infringement results from a
modification of the Software by the Customer or by a third party, from a
combination with elements Sensor Factory did not supply, from a use not in
accordance with this License Agreement, from continued use of a version not
supported within the meaning of Article 8.4, from continued use of the element
concerned after Sensor Factory has made available a corrected or replacement
version or has invited the Customer in writing to cease its use, or from a
third-party component within the meaning of Article 10.

11.6 — The provisions of this article constitute Sensor Factory's sole warranty in
respect of **third-party** intellectual property rights and, subject to that,
**exhaust the Customer's remedies** in that respect. They operate within the limit
of the liability cap set by the GTC and, should that cap not apply, within the
limit of the sums actually received by Sensor Factory under this License Agreement
in the twelve (12) months preceding the triggering event.

This article **neither limits nor excludes** the warranty owed by Sensor Factory
by reason of its own act (*fait personnel*), nor its liability in the event of
wilful misconduct (*dol*) or gross negligence (*faute lourde*).

## Article 12 — Data

12.1 — The Software collects technical data on the systems the Customer designates
to it and transmits them to the destinations the Customer configures. Sensor
Factory is the recipient of none of those data, unless the Customer expressly
configures an output to a platform operated by Sensor Factory, in which case the
terms of that platform apply in addition.

12.2 — The Customer remains the **controller** within the meaning of Regulation
2016/679 for the data the Agent collects. It is for the Customer to verify that
the probes it enables do not collect personal data beyond what its own analysis
permits.

Sensor Factory **receives none of those data**, processes them for no purpose of
its own and determines neither their purposes nor their means. It therefore acts
in respect of them **neither as controller nor as processor** within the meaning
of Article 28 of that Regulation. Article 12.2 of the GTC does not apply to those
data.

The Customer **undertakes not to configure the Agent so as to transmit personal
data to Sensor Factory**. Should it contemplate a configuration having that
effect, it shall inform Sensor Factory in writing and in advance, and the parties
shall agree the provisions required by Article 28 of Regulation 2016/679 before it
is implemented.

**By way of exception**, if a version of the Software implements a mechanism
within the meaning of Article 6.8, Sensor Factory is the **controller** for the
sole data that mechanism transmits to it for the purposes of license control and
invoicing. It informs the Customer thereof in the release notes and in its privacy
policy.

12.3 — **Customer's indemnity.** The Customer shall indemnify Sensor Factory
against any claim by a third party or by a data subject founded on the data the
Agent collects, carries or presents under the Customer's configuration, or on the
systems it has designated to the Agent, and shall bear the financial consequences
thereof.

If a supervisory authority opens proceedings against Sensor Factory concerning
those same data, the Customer shall promptly provide the information and
assistance necessary to its defence and **shall bear the defence costs incurred**.
Financial penalties imposed by an authority remain borne by the party on which
they are imposed.

This indemnity operates on the conditions of Article 11.3, applied for the benefit
of Sensor Factory.

## Article 13 — Compliance verification

13.1 — For as long as the control described in Article 6 does not measure usage,
compliance rests first on the Customer's declaration provided for in Article 4.4.
The Customer is bound by the accuracy of that declaration.

13.2 — Sensor Factory may, **once per twelve (12) month period** and on **thirty
(30) days'** notice, request from the Customer a written attestation of the
subscribed level and of the scope of use, signed by a duly authorised person.

13.3 — **Verification.** Where there is serious indication of use exceeding the
subscribed scope, Sensor Factory may have a verification carried out, by itself or
by an independent third party bound to secrecy, on **thirty (30) days'** notice.
That verification takes place during business hours, without disrupting
operations, and bears only on the elements necessary to establish the scope. It
opens no access to the data the Software collects on the Customer's behalf.

The verification proceeds from the inventories and logs the Customer holds,
according to a method Sensor Factory communicates to it with the notice. **It
requires the execution of no program supplied by Sensor Factory within the
Customer's information system.** Its conclusions are established on an adversarial
basis.

13.4 — **Costs and adjustment.** The costs of verification are borne by Sensor
Factory. They fall to the Customer where the verification establishes use
exceeding the Subscribed Scope beyond the tolerance provided for in Article 4.3.

In that case, **or where the Customer has omitted the information provided for in
Article 4.3**, the Customer shall pay the price difference for the period during
which the excess is established, and at most for the **twelve (12) months**
preceding the notification of the verification or of the omission. The adjustment
is calculated on the financial terms of the accepted Order Form.

It is calculated at the list price in force, **increased by twenty per cent
(20%)**, where the excess arises from a **knowingly inaccurate declaration** or
from the defeating of the mechanisms of Article 6.

Payment of that adjustment constitutes an extension of the Subscribed Scope for
the current period. **Sensor Factory may not subject the usage so regularised to a
further verification.**

13.5 — A deliberate breach of Articles 4.4, 13.2 or 13.3 constitutes a material
breach within the meaning of Article 16.

## Article 14 — Compliance: export control and probity

14.1 — The Customer undertakes to comply with French, European and, where
applicable, foreign regulations on export control and restrictive measures.

14.2 — It shall in particular not export, re-export, transfer or make available
the Software, directly or indirectly, for the benefit of a country, person or
entity subject to an embargo or asset-freezing measure, in particular under
European Union regulations, the *Export Administration Regulations* and the
sanctions programmes administered by the *Office of Foreign Assets Control* of the
United States, nor use it for a prohibited purpose, in particular in connection
with nuclear, chemical or biological weapons or their delivery systems.

14.3 — The Customer declares that it is not itself subject to any such measure and
shall inform Sensor Factory promptly should it become so. It shall indemnify
Sensor Factory against the consequences of a breach of this article, which
furthermore constitutes a material breach within the meaning of Article 16.
Financial penalties imposed by an authority remain borne by the party on which
they are imposed.

14.4 — **Probity.** Each party declares that it complies with the applicable
regulations on the prevention of corruption and influence peddling, in particular
French Law no. 2016-1691 of 9 December 2016, the *UK Bribery Act 2010* and the
*Foreign Corrupt Practices Act*, and shall not offer, promise or grant any payment
or advantage whatsoever with a view to obtaining an undue advantage. Each party
undertakes that its officers, employees and subcontractors involved under the
agreement observe the same rules. A breach of this article constitutes a material
breach within the meaning of Article 16.

## Article 15 — Assignment and change of control

15.1 — The Customer may not assign this License Agreement, nor the rights and
obligations arising from it, without the prior written consent of Sensor Factory.
In the event of a universal transfer of its assets and liabilities, the agreement
continues as of right with the transferee, subject to prior information of Sensor
Factory.

15.2 — Sensor Factory may assign this License Agreement to a company of its group
or to the acquirer of the business line concerned, subject to information of the
Customer.

15.3 — **One clarification is called for, the grant bearing on the whole Estate.**
In the event of a change of control of the Customer, the Estate covered remains
that which existed at the date of the transaction, together with its own organic
growth. Extension of the grant to the estate of the acquirer or of the entities it
otherwise controls is the subject of an amendment.

15.4 — The same applies in the other direction. Where the Customer takes control
of an entity during the term of the agreement, that entity's estate enters the
Estate only at the following renewal, on the basis of the declaration provided for
in Article 4.4 and of the financial terms arising from it.

## Article 16 — Termination

16.1 — Either party may terminate the agreement in the event of a material breach
by the other, not remedied within **thirty (30) days** of a formal notice to cure
(*mise en demeure*).

16.2 — **Suspension.** In the event of failure to pay when due, or of a breach of
Articles 4.5, 4.6, 13 or 14, Sensor Factory may suspend the issue of any new
License Token, with no formality other than written notification and without
prejudice to its right of termination.

16.3 — Termination entails the **immediate cessation of the right to use the
Licensed Probes**. The Customer shall cease such use and attest to it in writing
within thirty (30) days. It is not required to uninstall the Agent nor deprived of
the Free Probes, whose use falls under the Apache 2.0 license.

16.4 — Sums paid in respect of the current period remain acquired by Sensor
Factory. In the event of termination through its fault, it shall refund the
portion of the price corresponding to the unexpired period. **That refund is set
against any indemnity due, within the limits fixed by the GTC and restated in
Article 17.2.** Sums remaining due become immediately payable.

16.5 — **Survival.** There survive the end of the agreement, whatever the cause,
Articles 1.4, 2.4, 4.5 to 4.7, 8.5 to 8.7, 9 to 14 and 17, together with any
provision whose nature requires that it remain.

## Article 17 — Governing law, liability and language

17.1 — This License Agreement is governed by **French law**. The United Nations
Convention on Contracts for the International Sale of Goods of 11 April 1980 does
not apply to it.

17.2 — **Limitations of liability.** The provisions of the GTC relating to the
nature of Sensor Factory's obligation, to the exclusion of indirect loss, to the
liability cap, to the claim period, to force majeure and to dispute resolution
apply to this License Agreement. They cover the entirety of the obligations it
stipulates, including the undertakings, declarations and warranties it contains,
**as well as the conduct and indemnification obligations of Articles 11 and 12**.

In accordance with Article 1.4, they prevail over any contrary provision of any
other contractual document; any provision derogating from them without the express
waiver provided for in Article 1.4 is **of no effect**.

Should one of those provisions not apply, Sensor Factory's liability remains
limited to the sums it has actually received under this License Agreement in the
**twelve (12) months** preceding the triggering event.

The limitations and exclusions provided for in this article apply neither to
wilful misconduct (*dol*), nor to gross negligence (*faute lourde*), nor to
personal injury, nor to any liability which cannot be limited under the applicable
law.

The parties acknowledge that the financial terms of this License Agreement reflect
the allocation of risk resulting from this article, and that **Sensor Factory
would not have contracted without it**.

17.3 — **Force majeure.** Article 17 of the GTC applies to this agreement. The
following are in particular capable of constituting force majeure, **where they
meet the conditions of Article 1218 of the French Civil Code**: natural disasters,
fires and floods, wars, acts of terrorism and civil unrest, epidemics and the
health measures relating to them, decisions of a public authority, general or
sectoral strikes, large-scale cyberattacks, and prolonged interruption of public
telecommunications or energy supply networks.

17.4 — This License Agreement is drawn up in the **French language**. Sensor
Factory may provide a translation, in particular into English, for the Customer's
convenience. Such a translation has informational value only: **only the French
version is binding** between the parties, and it alone will be relied upon before
any court. In the event of divergence between the French version and a
translation, whatever the cause, the French version prevails.

---

## Signatures

Executed at ……………………………, on …………………………, in two original counterparts.

**For SENSOR FACTORY SAS** — Name: ………………………… Capacity: …………………………

**For the Customer** — Entity: ………………………… Name: ………………………… Capacity:
…………………………

This agreement may be signed electronically, on the terms of Articles 1366 and 1367
of the French Civil Code and of Regulation (EU) No 910/2014. It may be signed in
several separate counterparts, all of which together form one and the same
instrument.

---

## Annex 1 — Third-party components

This annex relates to a specific version of the Software. It is produced from the
build's dependency graph, and therefore describes what is actually linked into the
distributed binary rather than what the dependency file declares: a component used
by the tests alone does not appear in it.

The licenses below apply to those components alone and prevail, for them, over
this License Agreement, in accordance with Article 10.

The list of third-party components linked into the distributed binary, with their
respective licenses, is published and kept up to date at
[Third-party components](third-party.md).
